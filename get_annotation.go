package main

import (
	"context"
	"strconv"
	"strings"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (c *Client) getSubnet(namespace string) string {

	var cidr string

	// 设置要请求的 GVR
	gvr := schema.GroupVersionResource{
		Group:    "nci.yunshan.net",
		Version:  "v1",
		Resource: "subnets",
	}

	// 发送请求，并得到返回结果
	unStructData, err := c.dynamicClient.Resource(gvr).Namespace(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		panic(err.Error())
	}

	var obj Nets

	// 使用 runtime.DefaultUnstructuredConverter 转换 item 为对象
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unStructData.UnstructuredContent(), &obj)
	if err != nil {
		glog.Infof("Failed to convert item: %v\n", err)
		return "ERROR"
	}

	// 输出资源信息
	for _, item := range obj.Items {
		cidr = item.Spec.CIDR
	}

	if cidr == "" {
		glog.Infof("没有找到子网资源，请检查网络插件")
		return "ERROR"
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
