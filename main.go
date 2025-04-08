package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	ctx := context.TODO()

	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		panic(err)
	}

	for _, ns := range namespaces.Items {
		pods, err := clientset.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{})
		if err != nil {
			panic(err)
		}

		tscList := map[string]*corev1.TopologySpreadConstraint{}

		for _, pod := range pods.Items {
			for _, tsc := range pod.Spec.TopologySpreadConstraints {
				if tsc.WhenUnsatisfiable == corev1.ScheduleAnyway {
					continue
				}
				if tsc.TopologyKey == corev1.LabelHostname {
					continue
				}

				v, err := json.Marshal(tsc)
				if err != nil {
					panic(err)
				}

				h := sha1.Sum(v)
				hash := hex.EncodeToString(h[:])

				if _, ok := tscList[hash]; !ok {
					tscList[hash] = tsc.DeepCopy()
				}
			}
		}

		for _, tsc := range tscList {
			selector := metav1.FormatLabelSelector(tsc.LabelSelector)
			pods, err := clientset.CoreV1().Pods(ns.Name).List(ctx, metav1.ListOptions{
				LabelSelector: selector,
			})
			if err != nil {
				panic(err)
			}

			if len(pods.Items) <= 5 {
				continue
			}

			fmt.Printf("%s, maxSkew=%d, selector=%s\n", ns.Name, tsc.MaxSkew, selector)
			for _, pod := range pods.Items {
				fmt.Println("- " + pod.Name)
			}
			fmt.Println("")
		}
	}
}
