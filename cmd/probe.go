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
	"flag"

	"github.com/iorchard/asklepios/utils"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

// probeCmd represents the probe command
var probeCmd = &cobra.Command{
	Use:   "probe",
	Short: "Asklepios liveness/readiness probe",
	Run: func(cmd *cobra.Command, args []string) {
		probe()
	},
}

func init() {
	rootCmd.AddCommand(probeCmd)
}
func probe() {
	klog.InitFlags(nil)
	defer klog.Flush()
	flag.Parse()

	config = utils.KubeConfig()
	client, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}
	_, err := client.CoreV1().Nodes().
		List(ctx,
			metav1.ListOptions{
				LabelSelector: "node-role.kubernetes.io/control-plane=",
			})
	if err != nil {
		panic(err.Error())
	} else {
		klog.V(0).InfoS("Succeeded to probe the kubernetes nodes")
	}
}
