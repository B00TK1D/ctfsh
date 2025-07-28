package main

import (
	"context"
	"fmt"
	"log"
	"os/exec"
	"time"

	charmssh "github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	wishtea "github.com/charmbracelet/wish-ssh/tea"
	tea "github.com/charmbracelet/bubbletea"
	v1 "k8s.io/api/core/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	containerImage = "kalilinux/kali-rolling"
	podPort        = 3001
	namespace      = "default"
)

func main() {
	s, err := wish.NewServer(
		wish.WithAddress(":2222"),
		wish.WithHostKeyPath(".ssh/host_key"),
		wish.WithMiddleware(wishtea.Middleware(teaHandler)),
	)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Starting SSH server on :2222...")
	log.Fatal(s.ListenAndServe())
}

// teaHandler runs a Bubbletea model showing container logs.
func teaHandler(s charmssh.Session) (tea.Model, []tea.ProgramOption) {
	username := s.User()
	ctx := s.Context()
	log.Printf("User connected: %s", username)

	k8s, err := getClient()
	if err != nil {
		return errorModel(fmt.Sprintf("Kubernetes error: %v", err)), nil
	}

	podName := "kali-" + username

	if err := spawnPod(ctx, k8s, podName); err != nil {
		return errorModel(fmt.Sprintf("Pod start error: %v", err)), nil
	}

	go waitAndTunnelPort(ctx, s, podName, podPort)

	return spinnerModel{
		message: fmt.Sprintf("Container '%s' is running.\nForwarding port %d...", podName, podPort),
		k8s:     k8s,
		podName: podName,
	}, []tea.ProgramOption{tea.WithAltScreen()}
}

// getClient returns in-cluster or out-of-cluster Kubernetes client
func getClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		// fallback to default kubeconfig
		config, err = rest.InClusterConfig()
		if err != nil {
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}

// spawnPod creates the Kali container
func spawnPod(ctx context.Context, client *kubernetes.Clientset, name string) error {
	_, err := client.CoreV1().Pods(namespace).Create(ctx, &v1.Pod{
		ObjectMeta: meta.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app": "kali",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "kali",
					Image: containerImage,
					Ports: []v1.ContainerPort{{ContainerPort: podPort}},
					Command: []string{
						"sh", "-c", fmt.Sprintf("while true; do echo 'Kali ready on %d'; sleep 5; done", podPort),
					},
				},
			},
			RestartPolicy: v1.RestartPolicyNever,
		},
	}, meta.CreateOptions{})
	return err
}

// waitAndTunnelPort waits for the pod to become ready and tunnels port to client
func waitAndTunnelPort(ctx context.Context, sess charmssh.Session, podName string, port int) {
	k8s, _ := getClient()

	// Wait until pod is ready
	for {
		select {
		case <-ctx.Done():
			return
		default:
			pod, err := k8s.CoreV1().Pods(namespace).Get(ctx, podName, meta.GetOptions{})
			if err == nil && pod.Status.Phase == v1.PodRunning {
				goto ready
			}
			time.Sleep(1 * time.Second)
		}
	}
ready:

	// Get pod IP
	pod, _ := k8s.CoreV1().Pods(namespace).Get(ctx, podName, meta.GetOptions{})
	ip := pod.Status.PodIP
	if ip == "" {
		log.Println("Failed to get pod IP")
		return
	}

	// Port forward using socat and SSH stdout/stderr
	cmd := exec.Command("socat", fmt.Sprintf("TCP-LISTEN:%d,reuseaddr,fork", port), fmt.Sprintf("TCP:%s:%d", ip, port))
	cmd.Stdout = sess
	cmd.Stderr = sess
	cmd.Run()
}

type spinnerModel struct {
	message string
	k8s     *kubernetes.Clientset
	podName string
}

func (m spinnerModel) Init() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type tickMsg struct{}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			// delete pod
			_ = m.k8s.CoreV1().Pods(namespace).Delete(context.Background(), m.podName, meta.DeleteOptions{})
			return m, tea.Quit
		}
	case tickMsg:
		return m, tea.Tick(time.Second, func(time.Time) tea.Msg {
			return tickMsg{}
		})
	}
	return m, nil
}

func (m spinnerModel) View() string {
	return fmt.Sprintf("\n[+] %s\nPress Ctrl+C to quit and remove your pod.\n", m.message)
}

// fallback model
func errorModel(msg string) tea.Model {
	return staticModel{msg: msg}
}

type staticModel struct{ msg string }

func (m staticModel) Init() tea.Cmd                       { return nil }
func (m staticModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) { return m, nil }
func (m staticModel) View() string                        { return "\n[!] " + m.msg + "\n" }
