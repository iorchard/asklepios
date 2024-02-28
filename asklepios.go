package main

import (
    "context"
    "encoding/json"
    "flag"
    "fmt"
    "os"
    "path"
    "time"

    "github.com/spf13/viper"

    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    taints "k8s.io/kubernetes/pkg/util/taints"
    types "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/rest"
    "k8s.io/client-go/tools/clientcmd"
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

func CheckSkipNode(client *kubernetes.Clientset, name string) bool {
    skipNode := false
    var skipNodeTaint = v1.Taint {
        Key: "node.kubernetes.io/asklepios",
        Value: "skip",
        Effect: v1.TaintEffectNoExecute,
    }
    // fetch node object
    node, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return skipNode
    }
    klog.V(4).InfoS("Got the node object", "node", name)
    if taints.TaintExists(node.Spec.Taints, &skipNodeTaint) {
        klog.V(0).InfoS("Skip the node (Reason: Node has SkipNode taint)",
          "node", node.Name,
          "taintKey", skipNodeTaint.Key,
          "taintValue", skipNodeTaint.Value)
        skipNode = true
    }
    return skipNode 


}
func TaintNode(client *kubernetes.Clientset, name string, taint bool) error {
    var newNode *v1.Node
    var updated bool
    var err error
    var noExecuteTaint = v1.Taint {
        Key: "node.kubernetes.io/out-of-service",
        Value: "nodeshutdown",
        Effect: v1.TaintEffectNoExecute,
        TimeAdded: &metav1.Time{Time: time.Now()},
    }
    var action string 
    // fetch node object
    node, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return err
    }
    klog.V(4).InfoS("Got the node object", "node", name)
    if taint && !taints.TaintExists(node.Spec.Taints, &noExecuteTaint) {
        action = "Add the taint"
        newNode, updated, err = taints.AddOrUpdateTaint(node, &noExecuteTaint)
    } else if !taint && taints.TaintExists(node.Spec.Taints, &noExecuteTaint) {
        action = "Remove the taint"
        newNode, updated, err = taints.RemoveTaint(node, &noExecuteTaint)
    } else {
        return nil
    }
    if err == nil && updated {
        _, err = client.CoreV1().Nodes().Update(ctx, 
            newNode, metav1.UpdateOptions{})
        if err == nil {
            klog.V(0).InfoS("Succeeded to process the node",
              "node", node.Name,
              "action", action,
            )
        }
    }
    return err
}
func CordonNode(client *kubernetes.Clientset,
                name string, cordon bool) error {
    var err error
    var action string = "Make the node schedulable"
    if cordon {
        action = "Make the node unschedulable"
    }
    node, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return err
    }
    doit := (node.Spec.Unschedulable && !cordon) || 
                (!node.Spec.Unschedulable && cordon)
    if doit {
        payload := []patchNodeSpec{{
            Op:     "replace",
            Path:   "/spec/unschedulable",
            Value:  cordon,
        }}
        bpayload, _ := json.Marshal(payload)
        _, err := client.CoreV1().Nodes().
            Patch(ctx, name, 
                types.JSONPatchType,
                bpayload,
                metav1.PatchOptions{},
                )
        if err == nil {
            klog.V(0).InfoS("Succeeded to process the node",
              "node", node.Name,
              "action", action,
            )
        }
    }
    return err
}
func main() {
    // Initialize Viper if config.yaml exists
    if _, err := os.Stat("config.yaml"); err == nil {
        viper.SetConfigName("config")
        viper.SetConfigType("yaml")
        viper.AddConfigPath(".")
        err := viper.ReadInConfig()
        if err != nil {
            panic(err.Error())
        }
    } else {
        viper.Set("sleep", 10)
        viper.Set("interval", 3)
        viper.Set("kickout", 60)
        viper.Set("kickin", 60)
    }
    // configuration values
    sleepSeconds := viper.GetInt("sleep")
    intervalSeconds := viper.GetInt("interval")
    kickoutSeconds := viper.GetInt64("kickout")
    kickinSeconds := viper.GetInt64("kickin")
    var (
        sleep time.Duration = time.Duration(sleepSeconds)*time.Second
        interval time.Duration = time.Duration(intervalSeconds)*time.Second
        kickout int64 = kickoutSeconds
        kickin int64 = kickinSeconds
    )

    klog.InitFlags(nil)
    defer klog.Flush()
    flag.Parse()
    klog.V(4).InfoS("Asklepios is starting")

    // try in-cluster config first
    klog.V(4).InfoS("Try in-cluster config")
    config, err = rest.InClusterConfig()
    if err != nil {
        klog.V(4).ErrorS(err, err.Error())
    } else {
        // try out-out-cluster config 
        home, err := os.UserHomeDir()
        if err != nil {
            panic(err.Error())
        } else {
            klog.V(4).InfoS("Home directory", "home", home)
            if _, err := os.Stat(path.Join(home, ".kube/config")); err == nil {
                config, err = clientcmd.BuildConfigFromFlags("",
                                path.Join(home, ".kube/config"))
                if err != nil {
                    panic(err.Error())
                }
                klog.V(4).InfoS("Found API server", "API-Server", config.Host)
            } else {
                panic(err.Error())
            }
        }
    }
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
            panic(err.Error())
        }
        kickoutThreshold := time.Now().Unix() - kickout
        //kickoutThresholdRFC3339 := time.Unix(kickoutThreshold, 0).
        //                            Format(time.RFC3339)
        kickinThreshold := time.Now().Unix() - kickin
        //kickinThresholdRFC3339 := time.Unix(kickinThreshold, 0).
        //                            Format(time.RFC3339)
        for _, node := range nodes.Items {
            if CheckSkipNode(client, node.Name) {
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
                            err := CordonNode(client, node.Name, true)
                            if err != nil {
                                klog.ErrorS(err, err.Error())
                            }
                            time.Sleep(interval)
                            // taint node.kubernetes.io/out-of-service
                            err2 := TaintNode(client, node.Name, true)
                            if err2 != nil {
                                klog.ErrorS(err, err.Error())
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
                            err := CordonNode(client, node.Name, false)
                            if err != nil {
                                klog.ErrorS(err, err.Error())
                            }
                            time.Sleep(interval)
                            // remove taint node.kubernetes.io/out-of-service
                            err2 := TaintNode(client, node.Name, false)
                            if err2 != nil {
                                klog.ErrorS(err, err.Error())
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
