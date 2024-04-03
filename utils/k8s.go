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
	types "k8s.io/apimachinery/pkg/types"
	taints "k8s.io/kubernetes/pkg/util/taints"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type patchNodeSpec struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value bool   `json:"value"`
}
type clusterPod struct {
	nodeName string
	aMap     map[string](int64)
}

var (
	ctx    = context.TODO()
	config *rest.Config
	client *kubernetes.Clientset
	err    error
)
var noExecuteTaint = v1.Taint{
	Key:       "node.kubernetes.io/out-of-service",
	Value:     "nodeshutdown",
	Effect:    v1.TaintEffectNoExecute,
	TimeAdded: &metav1.Time{Time: time.Now()},
}

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
	var skipNodeTaint = v1.Taint{
		Key:    "node.kubernetes.io/asklepios",
		Value:  "skip",
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

func findDuplicates(arrStruct []clusterPod) (bool, string) {
	var dupStruct []clusterPod
	var podToKill string
	var vPod, uPod string
	var vCreation, uCreation int64
	var dupFound = false
	for _, v := range arrStruct {
		for _, u := range dupStruct {
			if v.nodeName == u.nodeName {
				// which one is the latest one
				for vKey, vVal := range v.aMap {
					vPod, vCreation = vKey, vVal
				}
				for uKey, uVal := range u.aMap {
					uPod, uCreation = uKey, uVal
				}
				klog.V(4).InfoS("Got the pod info", "name", vPod,
					"creationTime", vCreation)
				klog.V(4).InfoS("Got the duplicate pod info", "name", uPod,
					"creationTime", uCreation)
				if vCreation > uCreation {
					podToKill = vPod
				} else {
					podToKill = uPod
				}
				dupFound = true
				break
			}
		}
		if dupFound {
			break
		}
		dupStruct = append(dupStruct, v)
	}
	return dupFound, podToKill
}

func RebalanceClusterPods(client *kubernetes.Clientset) error {
	var err error
	err = RebalanceRabbitmqPods(client)
	if err != nil {
		klog.ErrorS(err, err.Error())
	}
	err = RebalanceMariadbPods(client)
	if err != nil {
		klog.ErrorS(err, err.Error())
	}
	return err
}

func CheckRebalanceCondition(client *kubernetes.Clientset, numPods int) bool {
	var isReady bool = false
	// check all control nodes are ready
	nodes, err := client.CoreV1().Nodes().
		List(ctx,
			metav1.ListOptions{
				LabelSelector: "node-role.kubernetes.io/control-plane=",
			})
	if err != nil {
		klog.ErrorS(err, err.Error())
	}
	numHealthyNodes := 0
	for _, node := range nodes.Items {
		for _, cond := range node.Status.Conditions {
			if cond.Type == "Ready" {
				if !taints.TaintExists(node.Spec.Taints, &noExecuteTaint) {
					numHealthyNodes++
				}
			}
		}
	}
	if numHealthyNodes > 1 && numHealthyNodes >= numPods {
		isReady = true
	} else {
		klog.V(0).InfoS("Not enough healthy nodes to rebalance",
			"healthyNodes", numHealthyNodes, "Pods", numPods)
	}
	return isReady
}

func RebalanceMariadbPods(client *kubernetes.Clientset) error {
	var arrayMariadb []clusterPod
	mariadbLabelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"application": "mariadb",
			"component":   "server",
		},
	}
	pods, err := client.CoreV1().Pods("openstack").
		List(ctx, metav1.ListOptions{
			LabelSelector: labels.Set(mariadbLabelSelector.MatchLabels).
				String(),
		})
	if err != nil {
		klog.ErrorS(err, err.Error())
		return err
	}
	isReady := CheckRebalanceCondition(client, len(pods.Items))
	if !isReady {
		return nil
	}
	for _, pod := range pods.Items {
		r := clusterPod{}
		r.nodeName = pod.Spec.NodeName
		r.aMap = map[string]int64{pod.Name: pod.GetCreationTimestamp().Unix()}
		arrayMariadb = append(arrayMariadb, r)
	}
	dupFound, podToKill := findDuplicates(arrayMariadb)
	klog.V(0).InfoS("Check MariaDB duplicate pods", "dupFound", dupFound,
		"podToKill", podToKill)
	if dupFound {
		err := client.CoreV1().Pods("openstack").Delete(ctx, podToKill,
			metav1.DeleteOptions{})
		if err != nil {
			klog.ErrorS(err, err.Error())
		}
	}

	return err
}

func RebalanceRabbitmqPods(client *kubernetes.Clientset) error {
	var arrayRabbit []clusterPod
	rabbitmqLabelSelector := metav1.LabelSelector{
		MatchLabels: map[string]string{
			"application": "rabbitmq",
			"component":   "server",
		},
	}
	pods, err := client.CoreV1().Pods("openstack").
		List(ctx, metav1.ListOptions{
			LabelSelector: labels.Set(rabbitmqLabelSelector.MatchLabels).
				String(),
		})
	if err != nil {
		klog.ErrorS(err, err.Error())
		return err
	}
	isReady := CheckRebalanceCondition(client, len(pods.Items))
	if !isReady {
		return nil
	}
	for _, pod := range pods.Items {
		r := clusterPod{}
		r.nodeName = pod.Spec.NodeName
		r.aMap = map[string]int64{pod.Name: pod.GetCreationTimestamp().Unix()}
		arrayRabbit = append(arrayRabbit, r)
	}
	dupFound, podToKill := findDuplicates(arrayRabbit)
	klog.V(0).InfoS("Check RabbitMQ duplicate pods", "dupFound", dupFound,
		"podToKill", podToKill)
	if dupFound {
		err := client.CoreV1().Pods("openstack").Delete(ctx, podToKill,
			metav1.DeleteOptions{})
		if err != nil {
			klog.ErrorS(err, err.Error())
		}
	}

	return err
}
func TaintNode(client *kubernetes.Clientset, name string, taint bool) error {
	var newNode *v1.Node
	var updated bool
	var err error
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
			Op:    "replace",
			Path:  "/spec/unschedulable",
			Value: cordon,
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
			time.Sleep(time.Duration(3) * time.Second)
		}
	}
	return err
}
