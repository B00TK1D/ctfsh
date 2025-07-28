package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func main() {
	private, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatalf("failed to generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		log.Fatalf("failed to create signer: %v", err)
	}

	config := &ssh.ServerConfig{
		NoClientAuth: true,
	}
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", ":2222")
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	log.Println("Listening on :2222")

	for {
		nConn, err := listener.Accept()
		if err != nil {
			log.Println("failed to accept connection:", err)
			continue
		}

		go handleConn(nConn, config)
	}
}

func handleConn(nConn net.Conn, config *ssh.ServerConfig) {
	defer nConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, config)
	if err != nil {
		log.Println("failed to handshake:", err)
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() == "direct-tcpip" {
			go handleForwardedTCP(ch)
		} else {
			_ = ch.Reject(ssh.UnknownChannelType, "unsupported channel type")
		}
	}
}

func handleForwardedTCP(ch ssh.NewChannel) {
	channel, requests, err := ch.Accept()
	if err != nil {
		log.Println("failed to accept channel:", err)
		return
	}
	go ssh.DiscardRequests(requests)

	kaliHost, kaliPort, err := startKaliPod()
	if err != nil {
		log.Println("failed to start Kali pod:", err)
		channel.Close()
		return
	}

	target, err := net.Dial("tcp", fmt.Sprintf("%s:%d", kaliHost, kaliPort))
	if err != nil {
		log.Println("failed to connect to pod:", err)
		channel.Close()
		return
	}

	go io.Copy(target, channel)
	io.Copy(channel, target)
	target.Close()
	channel.Close()
}

func startKaliPod() (string, int, error) {
	log.Println("Spinning up Kali pod...")

	config, err := rest.InClusterConfig()
	if err != nil {
		return "", 0, fmt.Errorf("in-cluster config error: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", 0, fmt.Errorf("clientset error: %w", err)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "kali-",
			Namespace:    "default",
			Labels:       map[string]string{"app": "kali"},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "kali",
					Image: "kalilinux/kali-rolling",
					Args:  []string{"sleep", "3600"},
					Ports: []v1.ContainerPort{{ContainerPort: 22}},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}

	created, err := clientset.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("pod create error: %w", err)
	}

	for i := 0; i < 30; i++ {
		p, _ := clientset.CoreV1().Pods("default").Get(context.TODO(), created.Name, metav1.GetOptions{})
		if p.Status.Phase == v1.PodRunning && p.Status.PodIP != "" {
			log.Println("Kali pod is ready at", p.Status.PodIP)
			return p.Status.PodIP, 22, nil
		}
		time.Sleep(2 * time.Second)
	}

	return "", 0, fmt.Errorf("pod did not become ready")
}
