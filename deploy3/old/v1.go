package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	v1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/homedir"
	"net/http"
	"path/filepath"
)

const (
	image         = "lscr.io/linuxserver/kali-linux:latest"
	listenPort    = ":1337"
	containerPort = 3001
	namespace     = "default"
)

func main() {
	clientset, config := getClient()

	listener, err := net.Listen("tcp", listenPort)
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	log.Printf("Listening on %s...", listenPort)

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}

		go handleConn(conn, clientset, config)
	}
}

func getClient() (*kubernetes.Clientset, *rest.Config) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to local kubeconfig
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Fatalf("Failed to build kubeconfig: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create clientset: %v", err)
	}

	return clientset, config
}

func handleConn(conn net.Conn, clientset *kubernetes.Clientset, config *rest.Config) {
	defer conn.Close()
	ctx := context.Background()
	podName := "kali-" + rand.String(5)

	// 1. Create pod
	pod := &v1.Pod{
		ObjectMeta: meta.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    map[string]string{"app": "kali"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "kali",
					Image: image,
					Ports: []v1.ContainerPort{{ContainerPort: containerPort}},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	_, err := clientset.CoreV1().Pods(namespace).Create(ctx, pod, meta.CreateOptions{})
	if err != nil {
		log.Printf("Failed to create pod: %v", err)
		return
	}
	defer clientset.CoreV1().Pods(namespace).Delete(ctx, podName, meta.DeleteOptions{})

	// 2. Wait until running
	for {
		time.Sleep(1 * time.Second)
		p, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, meta.GetOptions{})
		if err != nil {
			log.Printf("Error getting pod status: %v", err)
			return
		}
		if p.Status.Phase == v1.PodRunning {
			break
		}
	}

	// 3. Port-forward to pod
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		log.Printf("spdy transport error: %v", err)
		return
	}

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward")

	serverURL := req.URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", serverURL)

	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})
	defer close(stopChan)

	pf, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", containerPort)}, stopChan, readyChan, nil, nil)
	if err != nil {
		log.Printf("Port-forward error: %v", err)
		return
	}

	go pf.ForwardPorts()

	<-readyChan
	ports, err := pf.GetPorts()
	if err != nil || len(ports) == 0 {
		log.Printf("Failed to get forwarded ports: %v", err)
		return
	}

	localPort := ports[0].Local
	target, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		log.Printf("Failed to connect to forwarded port: %v", err)
		return
	}
	defer target.Close()

	log.Printf("Forwarded client to pod %s", podName)

	// 4. Pipe connections
	go io.Copy(target, conn)
	io.Copy(conn, target)
	log.Printf("Connection closed for pod %s", podName)
}
