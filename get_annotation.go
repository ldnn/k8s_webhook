package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

func getSubnet(namespace string) string {

	var cidr string

	// 加载配置文件，生成 config 对象
	config, err := clientcmd.BuildConfigFromFlags("", "/root/.kube/config")
	if err != nil {
		panic(err.Error())
	}

	// 实例化 DynamicClient
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// 设置要请求的 GVR
	gvr := schema.GroupVersionResource{
		Group:    "nci.yunshan.net",
		Version:  "v1",
		Resource: "subnets",
	}

	// 发送请求，并得到返回结果
	unStructData, err := dynamicClient.Resource(gvr).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	var obj Nets

	// 使用 runtime.DefaultUnstructuredConverter 转换 item 为对象
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unStructData.UnstructuredContent(), &obj)
	if err != nil {
		fmt.Printf("Failed to convert item: %v\n", err)
		return "ERROR"
	}

	// 输出资源信息
	for _, item := range obj.Items {
		cidr = item.Spec.CIDR
	}

	return cidr

}

// 生成描述键值对
func createAnnotation(net string) map[string]string {
	ip := []string{}
	ips := make(map[string]string)
	net = strings.Split(net, "/")[0]
	tmp := strings.Split(net, ".")
	i, _ := strconv.Atoi(tmp[3])
	for j := 0; j < 15; j++ {
		i += 1
		tmp[3] = strconv.Itoa(i)
		ip = append(ip, strings.Join(tmp, "."))
	}

	ips["nci.yunshan.net/ips"] = strings.Join(ip, ",")

	return ips
}
