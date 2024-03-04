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
package utils

import (
    "context"
    "encoding/json"
    "os"
    "path"
    "time"

    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    taints "k8s.io/kubernetes/pkg/util/taints"
    types "k8s.io/apimachinery/pkg/types"

    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
    "k8s.io/client-go/rest"
    "k8s.io/klog/v2"
)

type patchNodeSpec struct {
    Op      string  `json:"op"`
    Path    string  `json:"path"`
    Value   bool    `json:"value"`
}

var (
    ctx = context.TODO()
    config *rest.Config
    client *kubernetes.Clientset
    err error
)

func KubeConfig() *rest.Config {
    // try in-cluster config first
    klog.V(4).InfoS("Try in-cluster config")
    config, err = rest.InClusterConfig()
    if err == nil {
        klog.V(4).InfoS("In-cluster config is working")
    } else {
        klog.V(4).InfoS("In-cluster config is not working")
        // try out-out-cluster config 
        klog.V(4).InfoS("Try out-of-cluster config ($HOME/.kube/config)")
        home, err := os.UserHomeDir()
        if err != nil {
            panic(err.Error())
        } else {
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
    return config
}

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
    klog.V(4).InfoS("Got the node info", "node", name)
    if taints.TaintExists(node.Spec.Taints, &skipNodeTaint) {
        klog.V(0).InfoS("Skip the node (Reason: Node has the Skip taint)",
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
        action = "Add the out-of-service taint"
        newNode, updated, err = taints.AddOrUpdateTaint(node, &noExecuteTaint)
    } else if !taint && taints.TaintExists(node.Spec.Taints, &noExecuteTaint) {
        action = "Remove the out-of-service taint"
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
            time.Sleep(time.Duration(3)*time.Second)
        }
    }
    return err
}
