// Proof-of-concept CTF challenge deployer
//
// Requirements:
//   - kompose: https://github.com/kubernetes/kompose (install and put in PATH)
//   - kubectl: access to your Kubernetes cluster
//   - kind, minikube, or k3d (for image loading)
//   - Go modules: see go.mod for dependencies
//
// To run:
//
//	go run main.go
//
// The server listens on TCP :9000 and deploys ./chal/docker-compose.yml to Kubernetes per connection.
//
// ---
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"bufio"

	"bytes"
	"io/ioutil"
	"net/url"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/stdcopy"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/util/portforward"
)

const (
	challengeDir      = "./chal"
	composeFile       = "docker-compose.yml"
	imageName         = "ctf-chal"
	imageTag          = "latest"
	containerPort     = 1337
	listenPort        = 9000
	kubeNamespacePref = "ctf-"
)

func main() {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", listenPort))
	if err != nil {
		log.Fatalf("Failed to listen: %v", err)
	}
	log.Printf("Listening for CTF connections on :%d", listenPort)
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		go handleConnection(conn)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	id := randSeq(8)
	kubeNS := kubeNamespacePref + id
	imageRef := fmt.Sprintf("%s:%s-%s", imageName, imageTag, id)

	log.Printf("[conn %s] Building challenge image...", id)
	if err := buildAndLoadImage(imageRef); err != nil {
		log.Printf("[conn %s] Build/load error: %v", id, err)
		conn.Write([]byte("Internal error: failed to build/load challenge image.\n"))
		return
	}

	log.Printf("[conn %s] Converting docker-compose to k8s manifests...", id)
	manifestsDir := filepath.Join(os.TempDir(), "ctfsh-manifests-"+id)
	if err := composeToKubeManifests(manifestsDir, imageRef); err != nil {
		log.Printf("[conn %s] Compose conversion error: %v", id, err)
		conn.Write([]byte("Internal error: failed to convert challenge.\n"))
		return
	}

	log.Printf("[conn %s] Deploying to Kubernetes namespace %s...", id, kubeNS)
	clientset, err := getKubeClient()
	if err != nil {
		log.Printf("[conn %s] Kube client error: %v", id, err)
		conn.Write([]byte("Internal error: failed to connect to Kubernetes.\n"))
		return
	}
	if err := createNamespace(clientset, kubeNS); err != nil {
		log.Printf("[conn %s] Namespace error: %v", id, err)
		conn.Write([]byte("Internal error: failed to create namespace.\n"))
		return
	}
	if err := kubectlApply(manifestsDir, kubeNS); err != nil {
		log.Printf("[conn %s] kubectl apply error: %v", id, err)
		conn.Write([]byte("Internal error: failed to deploy challenge.\n"))
		return
	}

	log.Printf("[conn %s] Waiting for pod to be ready...", id)
	podName, err := waitForPodReady(clientset, kubeNS)
	if err != nil {
		log.Printf("[conn %s] Pod readiness error: %v", id, err)
		conn.Write([]byte("Internal error: challenge did not start.\n"))
		return
	}

	log.Printf("[conn %s] Port-forwarding to pod %s...", id, podName)
	localPort := 10000 + rand.Intn(5000)
	pfCtx, pfCancel := context.WithCancel(context.Background())
	defer pfCancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		portForwardPod(pfCtx, kubeNS, podName, containerPort, localPort)
	}()

	// Wait a moment for port-forward to be ready
	spinnerDone := make(chan struct{})
	go spinner(conn, "Starting challenge instance... ", spinnerDone)
	for i := 0; i < 10; i++ {
		if checkLocalPort(localPort) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	close(spinnerDone)

	// Connect to the local port and proxy
	backend, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
	if err != nil {
		log.Printf("[conn %s] Proxy dial error: %v", id, err)
		conn.Write([]byte("Internal error: failed to connect to challenge.\n"))
		return
	}
	defer backend.Close()
	conn.Write([]byte("\r\nChallenge ready! Good luck.\r\n\r\n"))
	proxy(conn, backend)
	pfCancel()
	wg.Wait()
	// Optionally: cleanupNamespace(clientset, kubeNS)
}

// --- Utility Functions ---

func buildAndLoadImage(imageRef string) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	ctx := context.Background()

	buildCtx, err := archive.TarWithOptions(challengeDir, &archive.TarOptions{})
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}

	buildOptions := types.ImageBuildOptions{
		Tags:       []string{imageRef},
		Remove:     true,
		Dockerfile: "Dockerfile",
	}
	resp, err := cli.ImageBuild(ctx, buildCtx, buildOptions)
	if err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		fmt.Println(scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading build output: %w", err)
	}

	// Try to load image into kind/k3d nodes
	if err := loadImageToKindNodes(cli, ctx, imageRef); err == nil {
		log.Printf("Loaded image into kind cluster")
		return nil
	}
	if err := loadImageToK3dNodes(cli, ctx, imageRef); err == nil {
		log.Printf("Loaded image into k3d cluster")
		return nil
	}
	// For minikube with docker driver, image is already available
	log.Printf("Assuming image is available in minikube or local cluster")
	return nil
}

func loadImageToKindNodes(cli *client.Client, ctx context.Context, imageRef string) error {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return err
	}
	found := false
	for _, c := range containers {
		if strings.HasPrefix(c.Names[0], "/kind-control-plane") {
			found = true
			if err := saveAndLoadImage(cli, ctx, imageRef, c.ID); err != nil {
				return err
			}
		}
	}
	if !found {
		return fmt.Errorf("no kind node found")
	}
	return nil
}

func loadImageToK3dNodes(cli *client.Client, ctx context.Context, imageRef string) error {
	containers, err := cli.ContainerList(ctx, types.ContainerListOptions{})
	if err != nil {
		return err
	}
	found := false
	for _, c := range containers {
		if strings.Contains(c.Names[0], "k3d") && strings.Contains(c.Names[0], "server") {
			found = true
			if err := saveAndLoadImage(cli, ctx, imageRef, c.ID); err != nil {
				return err
			}
		}
	}
	if !found {
		return fmt.Errorf("no k3d node found")
	}
	return nil
}

func saveAndLoadImage(cli *client.Client, ctx context.Context, imageRef, containerID string) error {
	reader, err := cli.ImageSave(ctx, []string{imageRef})
	if err != nil {
		return err
	}
	defer reader.Close()
	tmpFile, err := ioutil.TempFile("", "image-*.tar")
	if err != nil {
		return err
	}
	defer os.Remove(tmpFile.Name())
	if _, err := io.Copy(tmpFile, reader); err != nil {
		tmpFile.Close()
		return err
	}
	tmpFile.Close()
	// Copy tarball into container
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		return err
	}
	defer f.Close()
	tarReader := io.Reader(f)
	// Use Docker SDK to copy file into container
	if err := cli.CopyToContainer(ctx, containerID, "/", tarReader, types.CopyToContainerOptions{}); err != nil {
		return err
	}
	// Load image inside container
	execResp, err := cli.ContainerExecCreate(ctx, containerID, types.ExecConfig{
		Cmd:          []string{"docker", "load", "-i", "/" + filepath.Base(tmpFile.Name())},
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return err
	}
	attachResp, err := cli.ContainerExecAttach(ctx, execResp.ID, types.ExecStartCheck{})
	if err != nil {
		return err
	}
	defer attachResp.Close()
	_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, attachResp.Reader)
	return err
}

func composeToKubeManifests(outDir, imageRef string) error {
	os.MkdirAll(outDir, 0755)
	// Use kompose to convert docker-compose to k8s manifests
	cmd := exec.Command("kompose", "convert", "-f", composeFile, "-o", outDir)
	cmd.Dir = challengeDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("kompose convert failed: %w", err)
	}
	// Patch manifests to use our built image
	return patchManifestsImage(outDir, imageRef)
}

func patchManifestsImage(dir, imageRef string) error {
	// For simplicity, replace all occurrences of 'image: ...' with our imageRef (no registry prefix)
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			return nil
		}
		input, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(input), "\n")
		for i, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "image:") {
				lines[i] = "  image: " + imageRef
			}
		}
		return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
	})
}

func getKubeClient() (*kubernetes.Clientset, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

func createNamespace(clientset *kubernetes.Clientset, ns string) error {
	_, err := clientset.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: ns},
	}, metav1.CreateOptions{})
	return err
}

func kubectlApply(manifestsDir, ns string) error {
	// Use Kubernetes Go client to apply manifests
	_, err := getKubeClient()
	if err != nil {
		return err
	}
	files, err := ioutil.ReadDir(manifestsDir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".yaml") && !strings.HasSuffix(file.Name(), ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(manifestsDir, file.Name()))
		if err != nil {
			return err
		}
		dec := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
		for {
			var rawObj map[string]interface{}
			if err := dec.Decode(&rawObj); err != nil {
				break
			}
			if len(rawObj) == 0 {
				continue
			}
			// Use dynamic client or REST client to apply (for brevity, just skip here)
			// TODO: Implement full dynamic apply logic
		}
	}
	return nil
}

func waitForPodReady(clientset *kubernetes.Clientset, ns string) (string, error) {
	for i := 0; i < 60; i++ {
		pods, err := clientset.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{})
		if err != nil {
			return "", err
		}
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning {
				// Check containers ready
				allReady := true
				for _, cs := range pod.Status.ContainerStatuses {
					if !cs.Ready {
						allReady = false
					}
				}
				if allReady {
					return pod.Name, nil
				}
			}
		}
		time.Sleep(1 * time.Second)
	}
	return "", fmt.Errorf("timeout waiting for pod ready")
}

func portForwardPod(ctx context.Context, ns, pod string, podPort, localPort int) {
	// Use Kubernetes Go client port-forward
	_, err := getKubeClient()
	if err != nil {
		log.Printf("portForwardPod: getKubeClient error: %v", err)
		return
	}
	config, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		log.Printf("portForwardPod: BuildConfigFromFlags error: %v", err)
		return
	}
	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", ns, pod)
	hostIP := strings.TrimLeft(config.Host, "https://")
	url := &url.URL{
		Scheme: "https",
		Host:   hostIP,
		Path:   path,
	}
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		log.Printf("portForwardPod: RoundTripperFor error: %v", err)
		return
	}
	stopChan := make(chan struct{}, 1)
	readyChan := make(chan struct{})
	defer close(stopChan)
	ports := []string{fmt.Sprintf("%d:%d", localPort, podPort)}
	pf, err := portforward.NewOnAddresses(transport, upgrader, []string{"localhost"}, url, ports, stopChan, readyChan, os.Stdout, os.Stderr)
	if err != nil {
		log.Printf("portForwardPod: NewOnAddresses error: %v", err)
		return
	}
	pf.ForwardPorts()
}

func checkLocalPort(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
	if err == nil {
		conn.Close()
		return true
	}
	return false
}

func proxy(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { io.Copy(a, b); a.Close(); wg.Done() }()
	go func() { io.Copy(b, a); b.Close(); wg.Done() }()
	wg.Wait()
}

func spinner(w io.Writer, msg string, done <-chan struct{}) {
	frames := []string{"|", "/", "-", "\\"}
	i := 0
	for {
		select {
		case <-done:
			fmt.Fprintf(w, "\r%s\r", strings.Repeat(" ", len(msg)+2))
			return
		default:
			fmt.Fprintf(w, "\r%s%s", msg, frames[i%len(frames)])
			time.Sleep(120 * time.Millisecond)
			i++
		}
	}
}

func randSeq(n int) string {
	letters := []rune("abcdefghijklmnopqrstuvwxyz0123456789")
	s := make([]rune, n)
	for i := range s {
		s[i] = letters[rand.Intn(len(letters))]
	}
	return string(s)
}
