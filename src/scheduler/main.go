package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"time"

	"encoding/json"
	"net/http"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const schedulerName = "custom-scheduler"
const endpointEnvName = "ENDPOINT"
const defaultEndpoint = "http://172.17.0.1:8081/resolve"

// Scheduler represents a kubernetes scheduler
type Scheduler struct {
	clientset  *kubernetes.Clientset
	podQueue   chan *v1.Pod
	nodeLister listersv1.NodeLister
}

// NodeDescriptor describes a cluster node for the custom scheduler
type NodeDescriptor struct {
	Name          string   `json:"name"`
	MemAvail      float64  `json:"memAvail"`
	CpuAvail      float64  `json:"cpuAvail"`
	PodsInNode    []string `json:"podsInNode"`
	Zone          string   `json:"zone"`
	IsControlNode string   `json:"isControlNode"`
	Size          string   `json:"size"`
}

// PodDescriptor describes a cluster node for the custom scheduler
type PodDescriptor struct {
	Name            string  `json:"name"`
	Namespace       string  `json:"namespace"`
	UID             string  `json:"UID"`
	CpuReqs         float64 `json:"cpuReqs"`
	CpuLimits       float64 `json:"cpuLimits"`
	MemoryReqs      float64 `json:"memoryReqs"`
	MemoryLimits    float64 `json:"memoryLimits"`
	ApplicationName string  `json:"applicationName"`
	IsQueue         string  `json:"isQueue"`
}

// ClusterDescriptor stores the list of nodes and pods
type ClusterDescriptor struct {
	Nodes []NodeDescriptor `json:"nodes"`
	Pods  []PodDescriptor  `json:"pods"`
}

// modPodToNode represents the pod assignment on a (single) node
type mapPodNode struct {
	NamePod      string    `json:"namePod"`
	NamespacePod string    `json:"namespacePod"`
	UID          types.UID `json:"UID"`
	NameNode     string    `json:"nameNode"`
}

// Deployment stores the list of all pod assignments on nodes
type Deployment struct {
	Deployment []mapPodNode `json:"deployment"`
}

// NewScheduler creates the custom scheduler
func NewScheduler(podQueue chan *v1.Pod, quit chan struct{}) Scheduler {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	return Scheduler{
		clientset:  clientset,
		podQueue:   podQueue,
		nodeLister: initInformers(clientset, podQueue, quit),
	}
}

// initInformers creates informers for getting information about nodes and pods
func initInformers(clientset *kubernetes.Clientset, podQueue chan *v1.Pod, quit chan struct{}) listersv1.NodeLister {
	factory := informers.NewSharedInformerFactory(clientset, 0)

	nodeInformer := factory.Core().V1().Nodes()
	nodeInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			node, ok := obj.(*v1.Node)
			if !ok {
				log.Println("this is not a node")
				return
			}
			log.Printf("New Node Added to Store in initInformers(): %s", node.GetName())

			memQuantity := node.Status.Allocatable[v1.ResourceMemory]
			totalMemAvail := int(memQuantity.Value())
			log.Printf("Resource Memory node in initInformers(): %d", totalMemAvail)

			cpuQuantity := node.Status.Allocatable[v1.ResourceCPU]
			totalCpuAvail := int(cpuQuantity.Value())
			log.Printf("Resource CPU node in initInformers(): %d", totalCpuAvail)
		},
	})

	podInformer := factory.Core().V1().Pods()
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			pod, ok := obj.(*v1.Pod)
			if !ok {
				log.Println("this is not a pod")
				return
			}
			if pod.Spec.NodeName == "" && pod.Spec.SchedulerName == schedulerName {
				podQueue <- pod
			}
		},
	})

	factory.Start(quit)
	return nodeInformer.Lister()
}

// main is the main entyr points. It creates the scheduler and starts scheduling pods
func main() {
	fmt.Println("I'm the custom-scheduler!")

	rand.Seed(time.Now().Unix())

	podQueue := make(chan *v1.Pod, 300)
	defer close(podQueue)

	quit := make(chan struct{})
	defer close(quit)

	scheduler := NewScheduler(podQueue, quit)
	//scheduler.SchedulePods()
	scheduler.SchedulePods2()
}

// createJSONPods creates a json representation of available pods
func (s *Scheduler) createJSONPods() (listStructPods []PodDescriptor, listPod []*v1.Pod, err error) {
	var podStruct PodDescriptor
	listStructPods = make([]PodDescriptor, 0)
	listPod = make([]*v1.Pod, 0) //list of pods

	fmt.Println("createJsonPods(): ")

	for p := range s.podQueue {

		fmt.Println("  Found a pod to insert in createJsonPods():", p.Namespace, "/", p.Name)

		m := p.GetLabels()
		fmt.Printf("Labels of pod : %+v\n", m)

		applicationName, ok := m["applicationName"]
		if !ok {
			applicationName = "app"
		}

		isQueue, ok := m["isQueue"]
		if !ok {
			isQueue = "0"
		}

		reqs, limits, err := podRequestsAndLimits(p)
		if err != nil {
			return nil, nil, err
		}

		cpuReqs, cpuLimits, memoryReqs, memoryLimits := reqs[v1.ResourceCPU], limits[v1.ResourceCPU], reqs[v1.ResourceMemory], limits[v1.ResourceMemory]
		cpuReqsVal := float64(cpuReqs.MilliValue())
		cpuLimitsVal := float64(cpuLimits.MilliValue())
		memoryReqsVal := float64(memoryReqs.Value())
		memoryLimitsVal := float64(memoryLimits.Value())

		podStruct = PodDescriptor{Name: p.Name, Namespace: p.Namespace, UID: string(p.UID), CpuReqs: cpuReqsVal, CpuLimits: cpuLimitsVal, MemoryReqs: memoryReqsVal,
			MemoryLimits: memoryLimitsVal, ApplicationName: applicationName, IsQueue: isQueue}
		listStructPods = append(listStructPods, podStruct)

		listPod = append(listPod, p)

		if len(s.podQueue) == 0 {
			fmt.Println("Queue is empty !")
			break
		}

	}
	log.Println("  createJsonPods() completed")
	return listStructPods, listPod, nil
}

// createJSONNodes creates a json representation of available nodes
func (s *Scheduler) createJSONNodes() (listStructNodes []NodeDescriptor, err error) {
	var nodeStruct NodeDescriptor
	listStructNodes = make([]NodeDescriptor, 0)

	fmt.Println("createJsonNode():")

	nodes, err := s.nodeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}

	for i := range nodes {
		log.Printf("  Found node: %s", nodes[i].GetName())
		cpuLimits, memoryLimits := s.utilizedResourceByNode(nodes[i])

		memQuantity := nodes[i].Status.Allocatable[v1.ResourceMemory]
		totalMemAvail := float64(memQuantity.Value()) - memoryLimits
		log.Printf("  - Resource Memory: node %f, allocated %f, available %f\n", float64(memQuantity.Value()), memoryLimits, totalMemAvail)

		cpuQuantity := nodes[i].Status.Allocatable[v1.ResourceCPU]
		totalCPUAvail := float64(cpuQuantity.Value())*1000.0 - cpuLimits
		log.Printf("  - Resource CPU: node %f, allocated %f, available %f\n", float64(cpuQuantity.Value()*1000.0), cpuLimits, totalCPUAvail)

		m := nodes[i].GetLabels()
		fmt.Printf("Labels of node : %+v\n", m)

		zone, ok := m["zone"]
		if !ok {
			zone = "0"
		}

		isControlNode, ok := m["isControlNode"]
		if !ok {
			isControlNode = "0"
		} else {
			// ignore controller nodes
			continue
		}

		faasRole, ok := m["faasRole"]
		if ok && faasRole == "gateway" {
			// ignore gateway nodes
			continue
		}

		size, ok := m["size"]
		if !ok {
			size = "S"
		}

		podsInNode := s.podsInNode(nodes[i])

		nodeStruct = NodeDescriptor{Name: nodes[i].GetName(), MemAvail: totalMemAvail, CpuAvail: totalCPUAvail, Zone: zone, IsControlNode: isControlNode, Size: size, PodsInNode: podsInNode}
		listStructNodes = append(listStructNodes, nodeStruct)
	}

	log.Println("  createJsonNode() completed")
	return listStructNodes, nil
}

func (s *Scheduler) enqueuePod(deployment *mapPodNode, listPod []*v1.Pod) {
	for i := range listPod {
		if listPod[i].GetName() == deployment.NamePod {
			s.podQueue <- listPod[i]
			break
		}
	}
}

func (s *Scheduler) enqueueAllPods(listPod []*v1.Pod) {
	for i := range listPod {

		s.podQueue <- listPod[i]

	}
}

// SchedulePods2 retrieves pods and gets the deployment
func (s *Scheduler) SchedulePods2() error {

	var restServerEndpoint = os.Getenv(endpointEnvName)
	if len(restServerEndpoint) == 0 {
		restServerEndpoint = defaultEndpoint
	}
	fmt.Println("ENDPOINT:", restServerEndpoint)

	for p := range s.podQueue {
		s.podQueue <- p

		log.Println("SchedulePods running...")

		listTestNode, err := s.createJSONNodes()
		if err != nil {
			log.Println("cannot find nodes", err.Error())
			return err
		}
		listTestPod, listPodsToSchedule, err := s.createJSONPods()

		if err != nil {
			log.Println("cannot find pods", err.Error())
			return err
		}

		log.Println("Create Cluster JSON")
		clusterDescriptor := &ClusterDescriptor{
			Nodes: listTestNode,
			Pods:  listTestPod}
		clusterJSONDescriptor, _ := json.Marshal(clusterDescriptor)

		log.Println(string(clusterJSONDescriptor))

		resp, err := http.Post(restServerEndpoint, "application/json", bytes.NewBuffer(clusterJSONDescriptor))

		for err != nil {
			resp, err = http.Post(restServerEndpoint, "application/json", bytes.NewBuffer(clusterJSONDescriptor))
			fmt.Printf("Try HTTP Request again...\n")

		}

		fmt.Printf("Try HTTP Request: SUCCESS!\n")

		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)

		//if err != nil {
		//	panic(err)
		//}

		fmt.Println("Received: ", string(body))

		var computedDeployment Deployment
		deserializationError := json.Unmarshal(body, &computedDeployment)
		//if deserializationError != nil {
		//	fmt.Printf("Error while deserializing : %s\n", deserializationError)
		//	continue
		//}

		if err != nil || deserializationError != nil {
			fmt.Printf("Error while deserializing : %s\n", deserializationError)
			//enqueue all pods
			s.enqueueAllPods(listPodsToSchedule)

		} else {

			fmt.Printf("Deployment : %+v\n", computedDeployment)

			for _, podDeployment := range computedDeployment.Deployment {
				fmt.Printf("Deployment pod->node: %+v -> %+v", podDeployment.NamePod, podDeployment.NameNode)

				if podDeployment.NameNode == "NO_NODE_FOUND" {
					s.enqueuePod(&podDeployment, listPodsToSchedule)
					err = nil
				} else {

					err = s.bindPodToNode(&podDeployment)
				}
				if err != nil {
					log.Println("failed to bind pod", err.Error())
					continue
				}

				message := fmt.Sprintf("Placed pod [%s/%s] on %s\n", podDeployment.NamespacePod, podDeployment.NamePod, podDeployment.NameNode)

				err = s.emitEventUsingDeployment(&podDeployment, message)
				if err != nil {
					log.Println("failed to emit scheduled event", err.Error())
					continue
				}

				fmt.Println(message)
			}
		}

	}
	return nil

}

func (s *Scheduler) utilizedResourceByNode(node *v1.Node) (cpuLimitsVal, memoryLimitsVal float64) {

	pods, err := s.clientset.CoreV1().Pods("").List(metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + node.GetName() + ",spec.schedulerName=" + schedulerName,
	})

	if err != nil {
		panic(err.Error())
	}

	// get the sum of pods' requests and limits
	reqs, limits, err := getPodsTotalRequestsAndLimits(pods)
	if err != nil {
		panic(err.Error())
	}

	cpuReqs, cpuLimits, memoryReqs, memoryLimits := reqs[v1.ResourceCPU], limits[v1.ResourceCPU], reqs[v1.ResourceMemory], limits[v1.ResourceMemory]
	cpuReqsVal := float64(cpuReqs.MilliValue())
	cpuLimitsVal = float64(cpuLimits.MilliValue())
	memoryReqsVal := float64(memoryReqs.Value())
	memoryLimitsVal = float64(memoryLimits.Value())

	log.Printf("  cpuReqs: %d, cpuLimits: %d, memoryReqs: %d, memoryLimits: %d\n", int64(cpuReqsVal), int64(cpuLimitsVal), int64(memoryReqsVal), int64(memoryLimitsVal))

	return
}

func (s *Scheduler) podsInNode(node *v1.Node) (listPods []string) {

	pods, err := s.clientset.CoreV1().Pods("").List(metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + node.GetName() + ",spec.schedulerName=" + schedulerName,
	})
	if err != nil {
		panic(err.Error())
	}

	for _, pod := range pods.Items {
		listPods = append(listPods, pod.GetName())
	}

	return
}

// podRequestsAndLimits returns the resource requests and limits of a single pod
func podRequestsAndLimits(pod *v1.Pod) (reqs map[v1.ResourceName]resource.Quantity, limits map[v1.ResourceName]resource.Quantity, err error) {
	reqs, limits = map[v1.ResourceName]resource.Quantity{}, map[v1.ResourceName]resource.Quantity{}
	for _, container := range pod.Spec.Containers {
		for name, quantity := range container.Resources.Requests {
			if value, ok := reqs[name]; !ok {
				reqs[name] = *quantity.Copy()
			} else {
				value.Add(quantity)
				reqs[name] = value
			}
		}
		for name, quantity := range container.Resources.Limits {
			if value, ok := limits[name]; !ok {
				limits[name] = *quantity.Copy()
			} else {
				value.Add(quantity)
				limits[name] = value
			}
		}
	}
	// init containers define the minimum of any resource
	for _, container := range pod.Spec.InitContainers {
		for name, quantity := range container.Resources.Requests {
			value, ok := reqs[name]
			if !ok {
				reqs[name] = *quantity.Copy()
				continue
			}
			if quantity.Cmp(value) > 0 {
				reqs[name] = *quantity.Copy()
			}
		}
		for name, quantity := range container.Resources.Limits {
			value, ok := limits[name]
			if !ok {
				limits[name] = *quantity.Copy()
				continue
			}
			if quantity.Cmp(value) > 0 {
				limits[name] = *quantity.Copy()
			}
		}
	}
	return
}

// getPodsTotalRequestsAndLimits sums the requests and limits of a list of pods.
func getPodsTotalRequestsAndLimits(podList *v1.PodList) (reqs map[v1.ResourceName]resource.Quantity, limits map[v1.ResourceName]resource.Quantity, err error) {
	reqs, limits = map[v1.ResourceName]resource.Quantity{}, map[v1.ResourceName]resource.Quantity{}
	for _, pod := range podList.Items {
		if pod.Status.Phase == "Running" {
			podReqs, podLimits, err := podRequestsAndLimits(&pod)
			if err != nil {
				return nil, nil, err
			}
			for podReqName, podReqValue := range podReqs {
				if value, ok := reqs[podReqName]; !ok {
					reqs[podReqName] = *podReqValue.Copy()
				} else {
					value.Add(podReqValue)
					reqs[podReqName] = value
				}
			}
			for podLimitName, podLimitValue := range podLimits {
				if value, ok := limits[podLimitName]; !ok {
					limits[podLimitName] = *podLimitValue.Copy()
				} else {
					value.Add(podLimitValue)
					limits[podLimitName] = value
				}
			}
		}
	}
	return
}

func beforeBinding(randomNode *v1.Node) {
	var imagesOnNode []v1.ContainerImage
	imagesOnNode = randomNode.Status.Images

	log.Println("Before Binding pod-node")

	for i := range imagesOnNode {
		log.Printf("Name of image: %s and size: %d", imagesOnNode[i].Names[0], imagesOnNode[i].SizeBytes)
	}
}

func afterBinding(randomNode *v1.Node) {
	var imagesOnNode []v1.ContainerImage
	imagesOnNode = randomNode.Status.Images

	log.Println("After Binding pod-node")

	for i := range imagesOnNode {
		log.Printf("Name of image: %s and size: %d", imagesOnNode[i].Names[0], imagesOnNode[i].SizeBytes)
	}

	memQuantity := randomNode.Status.Allocatable[v1.ResourceMemory]
	totalMemAvail := int(memQuantity.Value())
	log.Printf("Resource Memory node: %d", totalMemAvail)

	cpuQuantity := randomNode.Status.Allocatable[v1.ResourceCPU]
	totalCPUAvail := int(cpuQuantity.Value())
	log.Printf("Resource CPU node: %d", totalCPUAvail)

}

func (s *Scheduler) findFit() (*v1.Node, error) {
	nodes, err := s.nodeLister.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	return nodes[rand.Intn(len(nodes))], nil
}

func (s *Scheduler) bindPod(p *v1.Pod, randomNode *v1.Node) error {
	return s.clientset.CoreV1().Pods(p.Namespace).Bind(&v1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      p.Name,
			Namespace: p.Namespace,
		},
		Target: v1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       randomNode.Name,
		},
	})
}
func (s *Scheduler) bindPodToNode(deployment *mapPodNode) error {

	return s.clientset.CoreV1().Pods(deployment.NamespacePod).Bind(&v1.Binding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deployment.NamePod,
			Namespace: deployment.NamespacePod,
		},
		Target: v1.ObjectReference{
			APIVersion: "v1",
			Kind:       "Node",
			Name:       deployment.NameNode,
		},
	})
}

func (s *Scheduler) emitEvent(p *v1.Pod, message string) error {
	timestamp := time.Now().UTC()
	_, err := s.clientset.CoreV1().Events(p.Namespace).Create(&v1.Event{
		Count:          1,
		Message:        message,
		Reason:         "Scheduled",
		LastTimestamp:  metav1.NewTime(timestamp),
		FirstTimestamp: metav1.NewTime(timestamp),
		Type:           "Normal",
		Source: v1.EventSource{
			Component: schedulerName,
		},
		InvolvedObject: v1.ObjectReference{
			Kind:      "Pod",
			Name:      p.Name,
			Namespace: p.Namespace,
			UID:       p.UID,
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: p.Name + "-",
		},
	})
	if err != nil {
		return err
	}
	return nil
}

func (s *Scheduler) emitEventUsingDeployment(p *mapPodNode, message string) error {
	timestamp := time.Now().UTC()
	_, err := s.clientset.CoreV1().Events(p.NamespacePod).Create(&v1.Event{
		Count:          1,
		Message:        message,
		Reason:         "Scheduled",
		LastTimestamp:  metav1.NewTime(timestamp),
		FirstTimestamp: metav1.NewTime(timestamp),
		Type:           "Normal",
		Source: v1.EventSource{
			Component: schedulerName,
		},
		InvolvedObject: v1.ObjectReference{
			Kind:      "Pod",
			Name:      p.NamePod,
			Namespace: p.NamespacePod,
			UID:       types.UID(p.UID),
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: p.NamePod + "-",
		},
	})
	if err != nil {
		return err
	}
	return nil
}
