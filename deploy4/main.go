package main

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

const (
	registryName      = "local-registry"
	registryNamespace = "kube-system"
	registryPort      = "5000"
	registryImage     = "registry:2"
)

type K8sDockerManager struct {
	kubeClient   *kubernetes.Clientset
	dockerClient *client.Client
	ctx          context.Context
}

func NewK8sDockerManager() (*K8sDockerManager, error) {
	ctx := context.Background()

	// Initialize Kubernetes client
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to build kubeconfig: %w", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Initialize Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	return &K8sDockerManager{
		kubeClient:   kubeClient,
		dockerClient: dockerClient,
		ctx:          ctx,
	}, nil
}

func (m *K8sDockerManager) CreateInternalRegistry() error {
	log.Println("Creating internal Docker registry...")

	// Check if deployment already exists
	existingDeployment, err := m.kubeClient.AppsV1().Deployments(registryNamespace).Get(m.ctx, registryName, metav1.GetOptions{})
	if err == nil {
		log.Printf("Registry deployment already exists, checking if it's ready...")
		if existingDeployment.Status.ReadyReplicas == *existingDeployment.Spec.Replicas {
			log.Println("Existing registry deployment is ready!")
			return m.ensureRegistryService()
		} else {
			log.Println("Existing registry deployment is not ready, waiting...")
			return m.waitForRegistryReady()
		}
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check existing deployment: %w", err)
	}

	// Create deployment for registry
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      registryName,
			Namespace: registryNamespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": registryName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": registryName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  registryName,
							Image: registryImage,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 5000,
									Name:          "registry",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "REGISTRY_STORAGE_DELETE_ENABLED",
									Value: "true",
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "registry-storage",
									MountPath: "/var/lib/registry",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "registry-storage",
							VolumeSource: corev1.VolumeSource{
								EmptyDir: &corev1.EmptyDirVolumeSource{},
							},
						},
					},
				},
			},
		},
	}

	_, err := m.kubeClient.AppsV1().Deployments(registryNamespace).Create(m.ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create registry deployment: %w", err)
	}

	// Create service for registry
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      registryName,
			Namespace: registryNamespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": registryName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "registry",
					Port:       5000,
					TargetPort: intstr.FromInt(5000),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	_, err = m.kubeClient.CoreV1().Services(registryNamespace).Create(m.ctx, service, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create registry service: %w", err)
	}

	log.Println("Registry service created")
	return nil
}

func (m *K8sDockerManager) waitForRegistryReady() error {
	log.Println("Waiting for registry deployment to be ready...")
	for i := 0; i < 60; i++ {
		deployment, err := m.kubeClient.AppsV1().Deployments(registryNamespace).Get(m.ctx, registryName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get deployment status: %w", err)
		}

		if deployment.Status.ReadyReplicas == *deployment.Spec.Replicas {
			log.Println("Registry deployment is ready!")
			return nil
		}

		time.Sleep(5 * time.Second)
	}

	return fmt.Errorf("registry deployment did not become ready within timeout")
}

func (m *K8sDockerManager) ConfigureNodesForRegistry() error {
	log.Println("Configuring nodes to use internal registry...")

	// Check if DaemonSet already exists
	_, err := m.kubeClient.AppsV1().DaemonSets(registryNamespace).Get(m.ctx, "registry-config", metav1.GetOptions{})
	if err == nil {
		log.Println("Registry configuration DaemonSet already exists")
		return nil
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check existing daemonset: %w", err)
	}

	// Create a DaemonSet to configure Docker daemon on all nodes
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "registry-config",
			Namespace: registryNamespace,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "registry-config",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "registry-config",
					},
				},
				Spec: corev1.PodSpec{
					HostNetwork: true,
					Containers: []corev1.Container{
						{
							Name:  "registry-config",
							Image: "alpine:latest",
							Command: []string{
								"sh",
								"-c",
								fmt.Sprintf(`
echo '{"insecure-registries": ["%s.%s.svc.cluster.local:5000"]}' > /host/etc/docker/daemon.json
sleep infinity
`, registryName, registryNamespace),
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: boolPtr(true),
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "docker-config",
									MountPath: "/host/etc/docker",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "docker-config",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/etc/docker",
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := m.kubeClient.AppsV1().DaemonSets(registryNamespace).Create(m.ctx, daemonSet, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create registry config daemonset: %w", err)
	}

	log.Println("Registry configuration applied to all nodes")
	return nil
}

func (m *K8sDockerManager) BuildAndPushImage(dockerfilePath, imageName string) error {
	log.Printf("Building and pushing image: %s", imageName)

	// Create build context
	buildContext, err := m.createBuildContext(dockerfilePath)
	if err != nil {
		return fmt.Errorf("failed to create build context: %w", err)
	}

	// Build image
	registryURL := fmt.Sprintf("%s.%s.svc.cluster.local:5000", registryName, registryNamespace)
	fullImageName := fmt.Sprintf("%s/%s", registryURL, imageName)

	buildOptions := types.ImageBuildOptions{
		Tags:           []string{fullImageName},
		Dockerfile:     "Dockerfile",
		Remove:         true,
		ForceRemove:    true,
		PullParent:     true,
		NoCache:        false,
		SuppressOutput: false,
	}

	buildResponse, err := m.dockerClient.ImageBuild(m.ctx, buildContext, buildOptions)
	if err != nil {
		return fmt.Errorf("failed to build image: %w", err)
	}
	defer buildResponse.Body.Close()

	// Read build output
	_, err = io.Copy(os.Stdout, buildResponse.Body)
	if err != nil {
		return fmt.Errorf("failed to read build output: %w", err)
	}

	// Push image to registry
	log.Printf("Pushing image to registry: %s", fullImageName)

	pushOptions := types.ImagePushOptions{}

	pushResponse, err := m.dockerClient.ImagePush(m.ctx, fullImageName, pushOptions)
	if err != nil {
		return fmt.Errorf("failed to push image: %w", err)
	}
	defer pushResponse.Close()

	// Read push output
	_, err = io.Copy(os.Stdout, pushResponse)
	if err != nil {
		return fmt.Errorf("failed to read push output: %w", err)
	}

	log.Printf("Successfully pushed image: %s", fullImageName)
	return nil
}

func (m *K8sDockerManager) DeployContainer(imageName, deploymentName string) error {
	log.Printf("Deploying container: %s", deploymentName)

	registryURL := fmt.Sprintf("%s.%s.svc.cluster.local:5000", registryName, registryNamespace)
	fullImageName := fmt.Sprintf("%s/%s", registryURL, imageName)

	// Check if deployment already exists
	existingDeployment, err := m.kubeClient.AppsV1().Deployments("default").Get(m.ctx, deploymentName, metav1.GetOptions{})
	if err == nil {
		log.Printf("Deployment %s already exists, updating image...", deploymentName)
		// Update the existing deployment with new image
		existingDeployment.Spec.Template.Spec.Containers[0].Image = fullImageName
		_, err = m.kubeClient.AppsV1().Deployments("default").Update(m.ctx, existingDeployment, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("failed to update existing deployment: %w", err)
		}
		return m.ensureDeploymentService(deploymentName)
	} else if !errors.IsNotFound(err) {
		return fmt.Errorf("failed to check existing deployment: %w", err)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: "default",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": deploymentName,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": deploymentName,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:            deploymentName,
							Image:           fullImageName,
							ImagePullPolicy: corev1.PullAlways,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: 8080,
									Name:          "http",
								},
							},
						},
					},
				},
			},
		},
	}

	_, err := m.kubeClient.AppsV1().Deployments("default").Create(m.ctx, deployment, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	// Create service for the deployment
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      deploymentName,
			Namespace: "default",
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": deploymentName,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}

	_, err = m.kubeClient.CoreV1().Services("default").Create(m.ctx, service, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create service: %w", err)
	}

	log.Printf("Successfully deployed container: %s", deploymentName)
	return nil
}

func (m *K8sDockerManager) createBuildContext(dockerfilePath string) (io.Reader, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// Get the directory containing the Dockerfile
	dockerfileDir := filepath.Dir(dockerfilePath)

	// Walk through all files in the directory
	err := filepath.Walk(dockerfileDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(dockerfileDir, path)
		if err != nil {
			return err
		}

		// Read file content
		fileContent, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Create tar header
		header := &tar.Header{
			Name: relPath,
			Size: int64(len(fileContent)),
			Mode: int64(info.Mode()),
		}

		// Write header and content
		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		if _, err := tw.Write(fileContent); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}

	return &buf, nil
}

func (m *K8sDockerManager) Cleanup() {
	if m.dockerClient != nil {
		m.dockerClient.Close()
	}
}

// Helper functions
func int32Ptr(i int32) *int32 {
	return &i
}

func boolPtr(b bool) *bool {
	return &b
}

func main() {
	manager, err := NewK8sDockerManager()
	if err != nil {
		log.Fatalf("Failed to initialize manager: %v", err)
	}
	defer manager.Cleanup()

	// Step 1: Create internal Docker registry
	if err := manager.CreateInternalRegistry(); err != nil {
		log.Fatalf("Failed to create internal registry: %v", err)
	}

	// Step 2: Configure nodes to use the registry
	if err := manager.ConfigureNodesForRegistry(); err != nil {
		log.Fatalf("Failed to configure nodes for registry: %v", err)
	}

	// Step 3: Build and push image (example usage)
	// You would replace these with actual values
	dockerfilePath := "./Dockerfile" // Path to your Dockerfile
	imageName := "my-app:latest"
	deploymentName := "my-app-deployment"

	if err := manager.BuildAndPushImage(dockerfilePath, imageName); err != nil {
		log.Fatalf("Failed to build and push image: %v", err)
	}

	// Step 4: Deploy the container
	if err := manager.DeployContainer(imageName, deploymentName); err != nil {
		log.Fatalf("Failed to deploy container: %v", err)
	}

	log.Println("All operations completed successfully!")
}
