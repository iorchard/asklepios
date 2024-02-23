package main

import (
    "context"
    "encoding/json"
    "fmt"
    "os"
    "path"
    "time"

    v1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    taints "k8s.io/kubernetes/pkg/util/taints"
    types "k8s.io/apimachinery/pkg/types"
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
)

type patchNodeSpec struct {
    Op      string  `json:"op"`
    Path    string  `json:"path"`
    Value   bool    `json:"value"`
}

var (
    ctx = context.Background()
    sleep time.Duration = 10*time.Second
    interval time.Duration = 3*time.Second
    kickout int64 = 60
    kickin int64 = 60
    noScheduleTaint = v1.Taint {
        Key: "node.kubernetes.io/test",
        Effect: v1.TaintEffectNoSchedule,
    }
    noExecuteTaint = v1.Taint {
        Key: "node.kubernetes.io/test",
        Effect: v1.TaintEffectNoSchedule,
    }
)

func TaintNode(client *kubernetes.Clientset, name string) error {
    // fetch node object
    node, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return err
    }
    if taints.TaintExists(node.Spec.Taints, &noScheduleTaint) {
        fmt.Println("taint already exists.")
    } else {
        newNode, updated, err := taints.AddOrUpdateTaint(node, &noScheduleTaint)
        if err == nil && updated {
            fmt.Printf("updated: %t\n", updated)
            _, err = client.CoreV1().Nodes().Update(ctx, 
                newNode, metav1.UpdateOptions{})
            fmt.Println(err)
        }
        return err
    }
    return nil
}
func UntaintNode(client *kubernetes.Clientset, name string) error {
    // fetch node object
    node, err := client.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
    if err != nil {
        return err
    }
    if !taints.TaintExists(node.Spec.Taints, &noScheduleTaint) {
        fmt.Println("taint does not exist.")
    } else {
        newNode, updated, err := taints.RemoveTaint(node, &noScheduleTaint)
        if err == nil && updated {
            fmt.Printf("updated: %t\n", updated)
            _, err = client.CoreV1().Nodes().Update(ctx, 
                newNode, metav1.UpdateOptions{})
            fmt.Println(err)
        }
        return err
    }
    return nil
}
func CordonNode(client *kubernetes.Clientset,
                name string, cordon bool) error {
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
    return err
}
func main() {
    home, err := os.UserHomeDir()
    if err != nil {
        panic(err)
    }
    fmt.Printf("Home directory: %s\n", home)

    config, err := clientcmd.BuildConfigFromFlags("", path.Join(home, ".kube/config"))
    if err != nil {
        panic(err.Error())
    }
    fmt.Printf("API-Server: %s\n", config.Host)

    client, err := kubernetes.NewForConfig(config)
    if err != nil {
        panic(err.Error())
    }
    for {
        // Get control node list
        nodes, _ := client.CoreV1().Nodes().
            List(ctx, 
                metav1.ListOptions{
                    LabelSelector:"node-role.kubernetes.io/control-plane=",
                })
        kickoutThreshold := time.Now().Unix() - kickout
        kickinThreshold := time.Now().Unix() - kickin
        for _, node := range nodes.Items {
            fmt.Printf("%s ", node.Name)
            for _, cond := range node.Status.Conditions {
                if cond.Type == "Ready" {
                    if cond.Status != v1.ConditionTrue {
                        fmt.Printf("%s %s (%d) (%d)\n", 
                            cond.Status, 
                            cond.LastTransitionTime,
                            cond.LastTransitionTime.Unix(),
                            kickoutThreshold,
                        )
                        if cond.LastTransitionTime.Unix() < kickoutThreshold {
                            // cordon the node
                            err := CordonNode(client, node.Name, true)
                            if err != nil {
                                panic(err.Error())
                            }
                            time.Sleep(interval)
                            // taint node.kubernetes.io/out-of-service
                            if !taints.TaintExists(node.Spec.Taints, &noScheduleTaint) {
                                err := TaintNode(client, node.Name)
                                if err != nil {
                                    panic(err.Error())
                                }
                            }
                        }
                    } else {
                        fmt.Printf("%s (%d) (%d) < (%d)\n",
                            cond.Status, 
                            cond.LastHeartbeatTime.Unix(),
                            cond.LastTransitionTime.Unix(),
                            kickinThreshold,
                        )
                        if cond.LastTransitionTime.Unix() < kickinThreshold {
                            // uncordon the node
                            err := CordonNode(client, node.Name, false)
                            if err != nil {
                                panic(err.Error())
                            }
                            time.Sleep(interval)
                            // remove taint node.kubernetes.io/out-of-service
                            fmt.Println(taints.TaintExists(node.Spec.Taints, &noScheduleTaint))
                            if taints.TaintExists(node.Spec.Taints, &noScheduleTaint) {
                                err := UntaintNode(client, node.Name)
                                if err != nil {
                                    panic(err.Error())
                                }
                            }
                        }
                    }
                }
            }
        }
        time.Sleep(sleep)
    }
}
