package main

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"path/filepath"
	"slices"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

type target struct {
	tsc          *corev1.TopologySpreadConstraint
	expectedPods []string
	actualPods   []string
}

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

	namespaceList := []string{}
	namespaces, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, ns := range namespaces.Items {
		namespaceList = append(namespaceList, ns.GetName())
	}
	sort.Strings(namespaceList)

	for _, ns := range namespaceList {
		targetList := map[string]*target{}

		pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			panic(err)
		}
		for _, pod := range pods.Items {
			for _, tsc := range pod.Spec.TopologySpreadConstraints {
				v, err := json.Marshal(tsc)
				if err != nil {
					panic(err)
				}

				h := sha1.Sum(v)
				hash := hex.EncodeToString(h[:])

				if _, ok := targetList[hash]; !ok {
					targetList[hash] = &target{
						tsc: tsc.DeepCopy(),
					}
				}
				targetList[hash].expectedPods = append(targetList[hash].expectedPods, pod.Name)
			}
		}

		for _, t := range targetList {
			selector := metav1.FormatLabelSelector(t.tsc.LabelSelector)
			pods, err := clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: selector})
			if err != nil {
				fmt.Printf("failed to list pods from tsc.labelSelector: ns=%s, selector=%s, %v\n", ns, selector, err)
				continue
			}

			actualPods := []string{}
			for _, pod := range pods.Items {
				actualPods = append(actualPods, pod.GetName())
			}
			sort.Strings(actualPods)
			t.actualPods = actualPods
		}

		hashList := []string{}
		for hash, _ := range targetList {
			hashList = append(hashList, hash)
		}
		sort.Strings(hashList)

		for _, hash := range hashList {
			t := targetList[hash]
			selector := metav1.FormatLabelSelector(t.tsc.LabelSelector)

			if slices.Compare(t.expectedPods, t.actualPods) != 0 {
				fmt.Println("Inconsistent TSC")
				fmt.Printf("- %s, %s, topologyKey=%s, maxSkew=%d, selector=%s\n", ns, t.tsc.WhenUnsatisfiable, t.tsc.TopologyKey, t.tsc.MaxSkew, selector)
				fmt.Printf("- expectedPods=%v\n", t.expectedPods)
				fmt.Printf("- actualPods  =%v\n", t.actualPods)
				fmt.Println("")
			} else if len(t.actualPods) > 5 && t.tsc.WhenUnsatisfiable == corev1.DoNotSchedule {
				fmt.Println("TooManyPods")
				fmt.Printf("- %s, %s, topologyKey=%s, maxSkew=%d, selector=%s\n", ns, t.tsc.WhenUnsatisfiable, t.tsc.TopologyKey, t.tsc.MaxSkew, selector)
				fmt.Printf("- pods=%v\n", t.actualPods)
				fmt.Println("")
			}
		}
	}
}
