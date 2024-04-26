package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/golang/glog"
)

func main() {
	var parameters WhSvrParameters

	// get command line parameters
	flag.IntVar(&parameters.port, "port", 443, "Webhook server port.")
	flag.StringVar(&parameters.certFile, "tlsCertFile", "/etc/webhook/certs/cert.crt", "File containing the x509 Certificate for HTTPS.")
	flag.StringVar(&parameters.keyFile, "tlsKeyFile", "/etc/webhook/certs/key.key", "File containing the x509 private key to --tlsCertFile.")
	flag.StringVar(&parameters.vpcprefix, "vpcprefix", "default", "vpcprefix")
	flag.StringVar(&parameters.cluster, "cluster", "poc", "cluster")
	flag.Var(&parameters.workspaces, "ws", "abnormal workspaces,for example:shanlv,tuangou")
	flag.Parse()

	if parameters.vpcprefix == " " {
		glog.Errorf("'vpcprefix'选项不支持空串!!!")
	}

	if parameters.cluster == " " {
		glog.Errorf("'cluster'选项不支持空串!!!")
	}

	pair, err := tls.LoadX509KeyPair(parameters.certFile, parameters.keyFile)
	if err != nil {
		glog.Errorf("Failed to load key pair: %v", err)
	}

	whsvr := &WebhookServer{
		server: &http.Server{
			Addr:      fmt.Sprintf(":%v", parameters.port),
			TLSConfig: &tls.Config{Certificates: []tls.Certificate{pair}},
		},
		vpcprefix:  parameters.vpcprefix,
		abnormalws: parameters.workspaces,
		cluster:    parameters.cluster,
	}

	// define http server and server handler
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", whsvr.serve)
	whsvr.server.Handler = mux

	// start webhook server in new routine
	go func() {
		if err := whsvr.server.ListenAndServeTLS("", ""); err != nil {
			glog.Errorf("Failed to listen and serve webhook server: %v", err)
		}
	}()

	glog.Info("Server started")

	// listening OS shutdown singal
	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	glog.Infof("Got OS shutdown signal, shutting down webhook server gracefully...")
	whsvr.server.Shutdown(context.Background())
}
