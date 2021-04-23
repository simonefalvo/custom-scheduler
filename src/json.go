package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
)

type pprovanode struct {
	Name     string `json:"name"`
	MemAvail int    `json:"memAvail"`
	CpuAvail int    `json:"cpuAvail"`
}

type pprovapod struct {
	Name         string `json:"name"`
	CpuReqs      int    `json:"cpuReqs"`
	CpuLimits    int    `json:"cpuLimits"`
	MemoryReqs   int    `json:"memoryReqs"`
	MemoryLimits int    `json:"memoryLimits"`
}

// Prova is a
type pprova struct {
	Nodes []pprovanode `json:"nodes"`
	Pods  []pprovapod  `json:"pods"`
}

// type response2 struct {
// 	Page   int      `json:"page"`
// 	Fruits []string `json:"fruits"`
// }

func main() {
	fmt.Println("Prova scrittura json: ")

	testNode := pprovanode{Name: "node", MemAvail: 1, CpuAvail: 2}
	testPod := pprovapod{Name: "pod", CpuReqs: 1, CpuLimits: 2, MemoryReqs: 10, MemoryLimits: 30}

	listTestNode := make([]pprovanode, 0)
	listTestPod := make([]pprovapod, 0)

	listTestNode = append(listTestNode, testNode)
	listTestPod = append(listTestPod, testPod)

	element := &pprova{
		Nodes: listTestNode,
		Pods:  listTestPod}

	eljson, _ := json.Marshal(element)

	fmt.Println(string(eljson))
	// fmt.Println("----")

	// res2D := &response2{
	// 	Page: 1}
	// res2B, _ := json.Marshal(res2D)
	// fmt.Println(string(res2B))

	// APIURL := "http://localhost:8081/greeting?name=prova"
	// req, err := http.NewRequest(http.MethodGet, APIURL, nil)

	APIURL := "http://localhost:8081/godemo"
	resp, err := http.Post(APIURL, "application/json", bytes.NewBuffer(eljson))
	if err != nil {
		panic(err)
	}

	// client := http.DefaultClient
	// resp, err := client.Do(req)
	// if err != nil {
	// 	panic(err)
	// }

	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err)
	}

	fmt.Println("Received: ", string(body))
}
