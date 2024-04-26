package main

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	v1 "k8s.io/api/admission/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func vpcHandler(wsName string, svmate serverMate) *v1.AdmissionResponse {

	//vpcName := "k8s-xpq-csy-poc-test"

	client := svmate.client
	var vpcName string
	label := make(map[string]string)

	label["kubesphere.io/cluster"] = svmate.cluster
	label["kubesphere.io/workspace"] = wsName

	if svmate.vpcprefix == "default" {
		return &v1.AdmissionResponse{
			Allowed: true,
		}
	} else {
		vpcName = generateVpcName(wsName, svmate)
	}

	switch svmate.op {
	case "DELETE":
		//workspace删除时同步删除vpc
		if !client.chekVpc(vpcName) || client.delVpc(vpcName) {
			return &v1.AdmissionResponse{
				Allowed: true,
			}
		}
	case "CREATE":
		//workspace创建时同步创建vpc
		if client.chekVpc(vpcName) || client.createVpc(vpcName, label) {
			return &v1.AdmissionResponse{
				Allowed: true,
			}
		}
	default:
		// 其他操作
		return &v1.AdmissionResponse{
			Allowed: true,
		}
	}

	msg := fmt.Sprintf("Vpc %s %s failed", vpcName, svmate.op)
	glog.Errorf(msg)
	return &v1.AdmissionResponse{
		Result: &metav1.Status{
			Message: msg,
		},
	}
}

func (c *Client) chekVpc(vpcName string) bool {

	// 设置要请求的 GVR
	gvr := schema.GroupVersionResource{
		Group:    "nci.yunshan.net",
		Version:  "v1",
		Resource: "vpcs",
	}

	// 发送请求，并得到返回结果
	unStructData, err := c.dynamicClient.Resource(gvr).Get(context.TODO(), vpcName, metav1.GetOptions{})
	if err != nil {
		glog.Error(err.Error())
		return false
	}

	var obj Vpc

	// 使用 runtime.DefaultUnstructuredConverter 转换 item 为对象
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unStructData.UnstructuredContent(), &obj)
	if err != nil {
		glog.Error(err.Error())
		//return false

	}

	return true
}

func (c *Client) createVpc(vpcName string, label map[string]string) bool {

	// 设置要请求的 GVR
	gvr := schema.GroupVersionResource{
		Group:    "nci.yunshan.net",
		Version:  "v1",
		Resource: "vpcs",
	}

	vpc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "nci.yunshan.net/v1",
			"kind":       "VPC",
			"metadata": map[string]interface{}{
				"name":   vpcName,
				"labels": label,
			},
		},
	}

	// 发送请求，并得到返回结果
	unStructData, err := c.dynamicClient.Resource(gvr).Create(context.TODO(), vpc, metav1.CreateOptions{})
	if err != nil {
		glog.Error(err.Error())
		return false
	}
	glog.Info("%v", unStructData)
	return true
}

func (c *Client) delVpc(vpcName string) bool {

	// 设置要请求的 GVR
	gvr := schema.GroupVersionResource{
		Group:    "nci.yunshan.net",
		Version:  "v1",
		Resource: "vpcs",
	}

	// 发送请求，并得到返回结果
	err := c.dynamicClient.Resource(gvr).Delete(context.TODO(), vpcName, metav1.DeleteOptions{})
	if err != nil {
		glog.Error(err.Error())
		return false
	}
	return true
}
