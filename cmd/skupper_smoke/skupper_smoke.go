package main

import (
	//"context"
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func sendReceive(ip string) {
	strEcho := "Halo"
	servAddr := ip + ":9090"
	tcpAddr, err := net.ResolveTCPAddr("tcp", servAddr)
	if err != nil {
		log.Fatalln("ResolveTCPAddr failed:", err.Error())
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		log.Fatalln("Dial failed:", err.Error())
	}
	_, err = conn.Write([]byte(strEcho))
	if err != nil {
		log.Fatalln("Write to server failed:", err.Error())
	}

	reply := make([]byte, 1024)

	_, err = conn.Read(reply)
	if err != nil {
		log.Fatalln("Write to server failed:", err.Error())
	}
	conn.Close()

	println("===== Sent to server=", strings.ToUpper(strEcho))
	println("===== Reply from server=", string(reply))

	if !strings.Contains(string(reply), strings.ToUpper(strEcho)) {
		log.Fatalf("Response from server different that expected: %s", string(reply))
	}
}

func _exec(command string) {
	fmt.Println(command)
	output, err := exec.Command("sh", "-c", command).CombinedOutput()
	if err != nil {
		panic(err)
		//os.Stderr.WriteString(err.Error())
	}
	fmt.Println(string(output))
}

func _setup(clientset *kubernetes.Clientset) {
	//publicNsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "public"}}
	//privateNsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "private"}}

	//deploymentsClient := clientset.AppsV1().Deployments(apiv1.NamespaceDefault)
	//_, err = clientset.CoreV1().Namespaces().Create(publicNsSpec)
	//if err != nil {
	//panic(err)
	//}

	//_, err = clientset.CoreV1().Namespaces().Create(privateNsSpec)
	//if err != nil {
	//panic(err)
	//}

	publicDeploymentsClient := clientset.AppsV1().Deployments("public")

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tcp-go-echo",
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"application": "tcp-go-echo"},
			},
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"application": "tcp-go-echo",
					},
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name:            "tcp-go-echo",
							Image:           "quay.io/skupper/tcp-go-echo",
							ImagePullPolicy: apiv1.PullIfNotPresent,
							Ports: []apiv1.ContainerPort{
								{
									Name:          "http",
									Protocol:      apiv1.ProtocolTCP,
									ContainerPort: 80,
								},
							},
						},
					},
				},
			},
		},
	}

	//result, err := deploymentsClient.Create(context.TODO(), deployment, metav1.CreateOptions{})
	fmt.Println("Creating deployment...")
	result, err := publicDeploymentsClient.Create(deployment)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())

	//TODO, wait for deployment state ready here?

	fmt.Printf("Listing deployments in namespace %q:\n", "public")
	list, err := publicDeploymentsClient.List(metav1.ListOptions{})
	if err != nil {
		panic(err)
	}
	for _, d := range list.Items {
		fmt.Printf(" * %s (%d replicas)\n", d.Name, *d.Spec.Replicas)
	}

	prompt("enter to start skupper magic..")

	//test launching skupper before deployment ready

	_exec("./skupper -n public init --cluster-local")
	_exec("./skupper -n public expose --port 9090 deployment tcp-go-echo")
	_exec("./skupper -n public connection-token /tmp/public_secret.yaml")

	_exec("./skupper -n private init --cluster-local")
	_exec("./skupper -n private connect /tmp/public_secret.yaml")
}

func _teardown(clientset *kubernetes.Clientset) {
	publicDeploymentsClient := clientset.AppsV1().Deployments("public")
	fmt.Println("Deleting deployment...")
	deletePolicy := metav1.DeletePropagationForeground
	if err := publicDeploymentsClient.Delete("tcp-go-echo", &metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}); err != nil {
		panic(err)
	}
	fmt.Println("Deleted deployment.")

	_exec("./skupper -n private delete")
	_exec("./skupper -n public delete")

	//err = clientset.CoreV1().Namespaces().Delete("public", &metav1.DeleteOptions{})
	//if err != nil {
	//panic(err)
	//}

	//err = clientset.CoreV1().Namespaces().Delete("private", &metav1.DeleteOptions{})
	//if err != nil {
	//panic(err)
	//}
}

//func initClientSet() *kubernetes.Clientset {
func initClientSet() {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kub    econfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	fmt.Printf("=====%v\n", *kubeconfig)

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	//_setup(clientset)

	_exec("kubectl -n private get svc")
	_exec("kubectl -n public get svc")

	//use "defer" for teardown
	//wait for:
	//kubectl -n public get svc
	//skupper-internal    ClusterIP   10.101.122.178   <none>        55671/TCP,45671/TCP   7m1s
	//skupper-messaging   ClusterIP   10.97.86.132     <none>        5671/TCP              7m1s
	//tcp-go-echo         ClusterIP   10.100.232.217   <none>        9090/TCP              34s

	//test tcp cliento, for public and private tcp-go-echo service

	prompt("setup completed, next run echo-test")
	var publicService *apiv1.Service
	var privateService *apiv1.Service
	publicService, err = clientset.CoreV1().Services("public").Get("tcp-go-echo", metav1.GetOptions{})
	if err != nil {
		panic(err)
	}

	privateService, err = clientset.CoreV1().Services("private").Get("tcp-go-echo", metav1.GetOptions{})
	if err != nil {
		panic(err)
	}

	fmt.Printf("======public ClusterIp = %q\n", publicService.Spec.ClusterIP)
	fmt.Printf("======private ClusterIp = %q\n", privateService.Spec.ClusterIP)
	fmt.Printf("======public\n")
	sendReceive(publicService.Spec.ClusterIP)
	fmt.Printf("======private\n")
	sendReceive(privateService.Spec.ClusterIP)

	prompt("enter to teardown")
	//_teardown(clientset)
}

func main() {
	initClientSet()
}

func int32Ptr(i int32) *int32 { return &i }

func prompt(message string) {
	fmt.Printf("%s\nPress Return key to continue.\n", message)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		break
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
	fmt.Println()
}
