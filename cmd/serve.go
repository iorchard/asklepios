/*
Copyright © 2024 Heechul Kim <jijisa@iorchard.net>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
    "context"
    "flag"
    "os"
    "time"

    "github.com/iorchard/asklepios/utils"
    "github.com/spf13/cobra"
    "github.com/spf13/viper"

    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/klog/v2"
)

type patchNodeSpec struct {
    Op      string  `json:"op"`
    Path    string  `json:"path"`
    Value   bool    `json:"value"`
}

var (
    ctx = context.Background()
    config *rest.Config
    client *kubernetes.Clientset
    err error
)

// serveCmd represents the serve command
var serveCmd = &cobra.Command{
    Use:   "serve",
    Short: "Check node status and execute an auto-healing process",
    Long: `Check node status and execute an auto-healing process
when a node is not ready`,
    Run: func(cmd *cobra.Command, args []string) {
        runAsklepios(cmd)
    },
}

func init() {
    rootCmd.AddCommand(serveCmd)
    serveCmd.Flags().StringP("config", "c", "config.yaml", 
        "asklepios config file path")
}

func runAsklepios(cmd *cobra.Command) {
    klog.InitFlags(nil)
    defer klog.Flush()
    flag.Parse()
    // Initialize Viper if conffile exists
    viper.SetDefault("sleep", 10)
    viper.SetDefault("kickout", 60)
    viper.SetDefault("kickin", 60)
    conffile, _ := cmd.Flags().GetString("config")
    _, err := os.Stat(conffile)
    if err != nil {
        klog.V(4).InfoS("Could not find config file", "config", conffile)
        klog.V(4).InfoS("Use the default config values",
            "sleep", viper.GetInt("sleep"),
            "kickout", viper.GetInt("kickout"),
            "kickin", viper.GetInt("kickin"))
    } else {
        klog.V(4).InfoS("Found config file", "config", conffile)
        viper.SetConfigType("yaml")
        viper.SetConfigFile(conffile)
        err := viper.ReadInConfig()
        if err != nil {
            panic(err.Error())
        }
    } 
    // configuration values
    sleepSeconds := viper.GetInt("sleep")
    kickoutSeconds := viper.GetInt64("kickout")
    kickinSeconds := viper.GetInt64("kickin")
    var (
        sleep time.Duration = time.Duration(sleepSeconds)*time.Second
        kickout int64 = kickoutSeconds
        kickin int64 = kickinSeconds
    )

    klog.V(4).InfoS("Asklepios service is starting")
    config = utils.KubeConfig()
    client, err = kubernetes.NewForConfig(config)
    if err != nil {
        panic(err.Error())
    }
    for {
        // Get control node list
        nodes, err := client.CoreV1().Nodes().
            List(ctx, 
                metav1.ListOptions{
                    LabelSelector:"node-role.kubernetes.io/control-plane=",
                })
        if err != nil {
            klog.ErrorS(err, err.Error())
            time.Sleep(sleep)
            continue
        }
        kickoutThreshold := time.Now().Unix() - kickout
        kickinThreshold := time.Now().Unix() - kickin
        for _, node := range nodes.Items {
            if utils.CheckSkipNode(client, node.Name) {
                continue
            }
            for _, cond := range node.Status.Conditions {
                if cond.Type == "Ready" {
                    ltt := cond.LastTransitionTime.Unix()
                    if cond.Status != v1.ConditionTrue {
                        if ltt < kickoutThreshold {
                            klog.V(0).InfoS("Node is not ready",
                              "node", node.Name,
                              "status", cond.Status,
                              "kickedOut", true)
                            // cordon the node
                            err := utils.CordonNode(client, node.Name, true)
                            if err != nil {
                                klog.ErrorS(err, err.Error())
                            }
                            // taint node.kubernetes.io/out-of-service
                            err2 := utils.TaintNode(client, node.Name, true)
                            if err2 != nil {
                                klog.ErrorS(err2, err2.Error())
                            }
                        } else {
                            tk := ltt - kickoutThreshold
                            klog.V(0).InfoS("Node is not ready",
                              "node", node.Name,
                              "status", cond.Status,
                              "kickedOut", false,
                              "timeToKickOut", tk)
                        }
                    } else {
                        if ltt < kickinThreshold {
                            klog.V(0).InfoS("Node is ready",
                              "node", node.Name,
                              "status", cond.Status,
                              "kickedIn", true)
                            // uncordon the node
                            err := utils.CordonNode(client, node.Name, false)
                            if err != nil {
                                klog.ErrorS(err, err.Error())
                            }
                            // remove taint node.kubernetes.io/out-of-service
                            err2 := utils.TaintNode(client, node.Name, false)
                            if err2 != nil {
                                klog.ErrorS(err2, err2.Error())
                            }
                            // move mariadb/rabbitmq to the recovered node
                            err3 := utils.RebalanceClusterPods(client)
                            if err3 != nil {
                                klog.ErrorS(err3, err3.Error())
                            }
                        } else {
                            tk := ltt - kickinThreshold
                            klog.V(0).InfoS("Node is ready",
                              "node", node.Name,
                              "status", cond.Status,
                              "kickedIn", false,
                              "timeToKickIn", tk)
                        }
                    }
                }
            }
        }
        time.Sleep(sleep)
    }
}
