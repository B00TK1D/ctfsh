package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/log"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/logging"
	gossh "golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/homedir"
)

const (
	host          = "localhost"
	port          = "23234"
	image         = "lscr.io/linuxserver/kali-linux:latest"
	containerPort = 3001
	namespace     = "default"
)

type directTCPChannelData struct {
	DestAddr   string
	DestPort   uint32
	OriginAddr string
	OriginPort uint32
}

type SessionData struct {
	PodName    string
	Clientset  *kubernetes.Clientset
	Config     *rest.Config
	Context    context.Context
	Cancel     context.CancelFunc
	Ready      chan struct{}
	ReadyOnce  sync.Once
	mu         sync.Mutex
}

var (
	globalClientset *kubernetes.Clientset
	globalConfig    *rest.Config
)

func init() {
	var err error
	globalClientset, globalConfig, err = getKubernetesClient()
	if err != nil {
		log.Fatal("Failed to initialize Kubernetes client", "error", err)
	}
}

func getKubernetesClient() (*kubernetes.Clientset, *rest.Config, error) {
	// Try in-cluster config first
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to local kubeconfig
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to build kubeconfig: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create clientset: %v", err)
	}

	return clientset, config, nil
}

func createKubernetesPod(ctx context.Context, clientset *kubernetes.Clientset, podName string) error {
	pod := &v1.Pod{
		ObjectMeta: meta.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels:    map[string]string{"app": "kali-ssh"},
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
	return err
}

func waitForPodReady(ctx context.Context, clientset *kubernetes.Clientset, podName string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, meta.GetOptions{})
			if err != nil {
				return fmt.Errorf("error getting pod status: %v", err)
			}
			if pod.Status.Phase == v1.PodRunning {
				return nil
			}
			time.Sleep(1 * time.Second)
		}
	}
}

func createPortForwarder(config *rest.Config, clientset *kubernetes.Clientset, podName string) (*portforward.PortForwarder, uint16, error) {
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, 0, fmt.Errorf("spdy transport error: %v", err)
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

	pf, err := portforward.New(dialer, []string{fmt.Sprintf("0:%d", containerPort)}, stopChan, readyChan, nil, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("port-forward error: %v", err)
	}

	go pf.ForwardPorts()

	<-readyChan
	ports, err := pf.GetPorts()
	if err != nil || len(ports) == 0 {
		return nil, 0, fmt.Errorf("failed to get forwarded ports: %v", err)
	}

	return pf, ports[0].Local, nil
}

func directTCPChannelHandler(srv *ssh.Server, conn *gossh.ServerConn, newChan gossh.NewChannel, ctx ssh.Context) {
	var payload directTCPChannelData
	if err := gossh.Unmarshal(newChan.ExtraData(), &payload); err != nil {
		log.Error("Failed to parse direct-tcpip payload", "error", err)
		newChan.Reject(gossh.ConnectionFailed, "failed to parse payload")
		return
	}

	log.Info("Direct TCP connection request", "dest", payload.DestAddr, "port", payload.DestPort)

	if srv.LocalPortForwardingCallback != nil && !srv.LocalPortForwardingCallback(ctx, payload.DestAddr, payload.DestPort) {
		newChan.Reject(gossh.Prohibited, "port forwarding is disabled")
		return
	}

	channel, requests, err := newChan.Accept()
	if err != nil {
		log.Error("Failed to accept channel", "error", err)
		return
	}
	defer channel.Close()

	go gossh.DiscardRequests(requests)

	// Get session data
	sessionData, ok := ctx.Value("sessionData").(*SessionData)
	if !ok {
		log.Error("No session data found")
		return
	}

	// Wait for pod to be ready
	select {
	case <-sessionData.Ready:
		// Pod is ready
	case <-sessionData.Context.Done():
		log.Error("Session context cancelled while waiting for pod")
		return
	}

	// Create port forwarder for this connection
	pf, localPort, err := createPortForwarder(sessionData.Config, sessionData.Clientset, sessionData.PodName)
	if err != nil {
		log.Error("Failed to create port forwarder", "error", err)
		return
	}
	defer pf.Close()

	// Connect to the forwarded port
	target, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		log.Error("Failed to connect to forwarded port", "error", err)
		return
	}
	defer target.Close()

	log.Info("Forwarding connection to pod", "pod", sessionData.PodName, "localPort", localPort)

	// Pipe the connections
	done := make(chan struct{}, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		io.Copy(target, channel)
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	go func() {
		defer wg.Done()
		io.Copy(channel, target)
		select {
		case done <- struct{}{}:
		default:
		}
	}()

	// Wait for either direction to close or session context to be done
	select {
	case <-done:
	case <-sessionData.Context.Done():
	}

	// Close connections to stop the other goroutine
	target.Close()
	channel.Close()

	// Wait for both goroutines to finish
	wg.Wait()

	log.Info("Connection closed", "pod", sessionData.PodName)
}

func sessionHandler(s ssh.Session) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	podName := "kali-" + rand.String(8)

	sessionData := &SessionData{
		PodName:   podName,
		Clientset: globalClientset,
		Config:    globalConfig,
		Context:   ctx,
		Cancel:    cancel,
		Ready:     make(chan struct{}),
	}

	// Store session data in context
	s.Context().SetValue("sessionData", sessionData)

	wish.Println(s, "🚀 Spinning up your Kubernetes environment...")
	wish.Println(s, "Pod name:", podName)

	// Create the pod
	err := createKubernetesPod(ctx, globalClientset, podName)
	if err != nil {
		wish.Printf(s, "❌ Failed to create pod: %v\n", err)
		return
	}

	// Clean up pod when session ends
	defer func() {
		log.Info("Cleaning up pod", "pod", podName)
		globalClientset.CoreV1().Pods(namespace).Delete(context.Background(), podName, meta.DeleteOptions{})
	}()

	// Wait for pod to be ready in a goroutine
	go func() {
		spinner := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinnerIdx := 0

		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				wish.Printf(s, "\r%s Waiting for pod to be ready...", spinner[spinnerIdx])
				spinnerIdx = (spinnerIdx + 1) % len(spinner)
			default:
				err := waitForPodReady(ctx, globalClientset, podName)
				if err != nil {
					wish.Printf(s, "\r❌ Failed to wait for pod: %v\n", err)
					return
				}

				ticker.Stop()
				wish.Printf(s, "\r✅ Pod is ready!                    \n")
				wish.Println(s, "")
				wish.Println(s, "🔗 You can now create port forwards:")
				wish.Println(s, "   ssh -L 5555:chal:0 user@host")
				wish.Println(s, "")
				wish.Println(s, "Press Ctrl+C to exit and cleanup the pod...")

				sessionData.ReadyOnce.Do(func() {
					close(sessionData.Ready)
				})
				return
			}
		}
	}()

	// Wait for user input or context cancellation
	c := make([]byte, 1)
	for {
		select {
		case <-ctx.Done():
			return
		default:
			_, err := s.Read(c)
			if err != nil {
				if errors.Is(err, io.EOF) {
					log.Info("Session closed")
					return
				}
				log.Error("Error reading from session", "error", err)
				return
			}
			if c[0] == 3 { // Ctrl+C
				wish.Println(s, "\n👋 Goodbye! Cleaning up your pod...")
				return
			}
		}
	}
}

func main() {
	s, err := wish.NewServer(
		wish.WithAddress(net.JoinHostPort(host, port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		func(s *ssh.Server) error {
			// Handle local port forwarding channels
			s.ChannelHandlers = map[string]ssh.ChannelHandler{
				"direct-tcpip": directTCPChannelHandler,
				"session":      ssh.DefaultSessionHandler,
			}
			return nil
		},
		wish.WithMiddleware(
			func(h ssh.Handler) ssh.Handler {
				return sessionHandler
			},
			logging.Middleware(),
		),
	)

	if err != nil {
		log.Error("Could not start server", "error", err)
		os.Exit(1)
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	log.Info("Starting SSH server", "host", host, "port", port)

	go func() {
		if err = s.ListenAndServe(); err != nil && !errors.Is(err, ssh.ErrServerClosed) {
			log.Error("Could not start server", "error", err)
			done <- nil
		}
	}()

	<-done
	log.Info("Stopping SSH server")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s.Shutdown(ctx)
}
