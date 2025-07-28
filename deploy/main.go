package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	charmssh "github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"

	"io/ioutil"
	"regexp"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	s, err := wish.NewServer(
		wish.WithAddress(":2222"),
		wish.WithMiddleware(challengeMiddleware),
	)
	if err != nil {
		log.Fatalf("Failed to start SSH server: %v", err)
	}
	log.Println("Listening on port 2222...")
	log.Fatal(s.ListenAndServe())
}

var challengeMiddleware wish.Middleware = func(next charmssh.Handler) charmssh.Handler {
	return func(sess charmssh.Session) {
		user := sess.User()
		ns := fmt.Sprintf("ctf-%s-%d", user, time.Now().Unix())

		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			wish.WriteString(sess, "Failed to load kubeconfig: "+err.Error()+"\n")
			return
		}
		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			wish.WriteString(sess, "Failed to init Kubernetes client: "+err.Error()+"\n")
			return
		}

		wish.WriteString(sess, "Creating challenge namespace...\n")
		_, err = clientset.CoreV1().Namespaces().Create(context.Background(), &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: ns},
		}, metav1.CreateOptions{})
		if err != nil {
			wish.WriteString(sess, "Namespace creation failed: "+err.Error()+"\n")
			return
		}

		wish.WriteString(sess, "Deploying challenge...\n")

		// Ensure chal/k8s/ exists and is populated
		k8sDir := "chal/k8s"
		composeFile := "chal/docker-compose.yml"
		needConvert := false
		if fi, err := os.Stat(k8sDir); err != nil || !fi.IsDir() {
			needConvert = true
		} else {
			entries, err := os.ReadDir(k8sDir)
			if err != nil || len(entries) == 0 {
				needConvert = true
			}
		}
		if needConvert {
			wish.WriteString(sess, "Converting docker-compose.yml to Kubernetes manifests...\n")
			cmd := exec.Command("kompose", "convert", "-f", composeFile, "-o", k8sDir+"/")
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			if err := cmd.Run(); err != nil {
				wish.WriteString(sess, "kompose convert failed:\n"+out.String()+"\n")
				return
			}
		}

		// Build and push Docker image to local registry
		wish.WriteString(sess, "Building challenge Docker image...\n")
		buildCmd := exec.Command("docker", "build", "-t", "localhost:5001/web:latest", "chal/")
		var buildOut bytes.Buffer
		buildCmd.Stdout = &buildOut
		buildCmd.Stderr = &buildOut
		if err := buildCmd.Run(); err != nil {
			wish.WriteString(sess, "Docker build failed:\n"+buildOut.String()+"\n")
			_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
			return
		}
		wish.WriteString(sess, "Pushing image to local registry...\n")
		pushCmd := exec.Command("docker", "push", "localhost:5001/web:latest")
		var pushOut bytes.Buffer
		pushCmd.Stdout = &pushOut
		pushCmd.Stderr = &pushOut
		if err := pushCmd.Run(); err != nil {
			wish.WriteString(sess, "Docker push failed:\n"+pushOut.String()+"\n")
			_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
			return
		}

		// Patch k8s manifests to use the local registry image
		wish.WriteString(sess, "Patching Kubernetes manifests to use local image...\n")
		files, err := ioutil.ReadDir(k8sDir)
		if err != nil {
			wish.WriteString(sess, "Failed to read k8s manifest directory: "+err.Error()+"\n")
			_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
			return
		}
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			path := filepath.Join(k8sDir, f.Name())
			data, err := ioutil.ReadFile(path)
			if err != nil {
				continue
			}
			str := string(data)
			if strings.Contains(str, "image:") {
				str = regexp.MustCompile(`image:.*`).ReplaceAllString(str, "image: localhost:5001/web:latest")
				ioutil.WriteFile(path, []byte(str), 0644)
			}
		}

		cmd := exec.Command("kubectl", "apply", "-n", ns, "-f", k8sDir+"/")
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			wish.WriteString(sess, "Failed to apply manifests:\n"+out.String()+"\n")
			return
		}

		// Spinner animation
		spinnerDone := make(chan struct{})
		go func() {
			frames := []string{"|", "/", "-", "\\"}
			i := 0
			for {
				select {
				case <-spinnerDone:
					return
				default:
					wish.WriteString(sess, "\rLoading "+frames[i%len(frames)])
					time.Sleep(150 * time.Millisecond)
					wish.WriteString(sess, "\r         \r")
					i++
				}
			}
		}()

		// Wait for pod to be ready
		var podName string
		ready := false
		for i := 0; i < 360; i++ { // up to 60*0.5=180s
			pods, err := clientset.CoreV1().Pods(ns).List(context.Background(), metav1.ListOptions{LabelSelector: "io.kompose.service=web"})
			if err == nil && len(pods.Items) > 0 {
				pod := pods.Items[0]
				podName = pod.Name
				for _, c := range pod.Status.ContainerStatuses {
					fmt.Printf("Container %s status: %s\n", c.Name, c.State.String())
					if c.Ready {
						ready = true
						break
					}
				}
			}
			if ready {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		close(spinnerDone)
		wish.WriteString(sess, "\r\n")
		if !ready {
			wish.WriteString(sess, "Challenge pod did not become ready in time.\n")
			_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
			return
		}

		// Start port-forward: 127.0.0.1:9000 -> pod:8000
		pfCmd := exec.Command("kubectl", "port-forward", "-n", ns, podName, "9000:8000", "--address", "127.0.0.1")
		pfStdout := &bytes.Buffer{}
		pfStderr := &bytes.Buffer{}
		pfCmd.Stdout = pfStdout
		pfCmd.Stderr = pfStderr
		if err := pfCmd.Start(); err != nil {
			wish.WriteString(sess, "Failed to start port-forward: "+err.Error()+"\n")
			_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
			return
		}
		// Wait for port-forward to be up
		pfReady := false
		for i := 0; i < 20; i++ {
			if strings.Contains(pfStdout.String(), "Forwarding from 127.0.0.1:9000") || strings.Contains(pfStderr.String(), "Forwarding from 127.0.0.1:9000") {
				pfReady = true
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		if !pfReady {
			wish.WriteString(sess, "Port-forward did not start successfully.\n")
			pfCmd.Process.Kill()
			_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
			return
		}

		// Print instructions
		wish.WriteString(sess, `
Challenge deployed!

Your local port 9000 is now forwarded to the challenge.
Open http://127.0.0.1:9000/flag in your browser.
(press Ctrl+C or disconnect to stop the challenge)
`)

		// Wait for disconnect
		<-sess.Context().Done()

		wish.WriteString(sess, "Cleaning up challenge instance...\n")
		if pfCmd.Process != nil {
			pfCmd.Process.Kill()
		}
		_ = clientset.CoreV1().Namespaces().Delete(context.Background(), ns, metav1.DeleteOptions{})
	}
}
