package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func int32Ptr(i int32) *int32 { return &i }

const minute time.Duration = 60

var deployment *appsv1.Deployment = &appsv1.Deployment{
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

func sendReceive(servAddr string) {
	strEcho := "Halo"
	//servAddr := ip + ":9090"
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

	log.Println("Sent to server = ", strEcho)
	log.Println("Reply from server = ", string(reply))

	if !strings.Contains(string(reply), strings.ToUpper(strEcho)) {
		log.Fatalf("Response from server different that expected: %s", string(reply))
	}
}

type ClusterContext struct {
	Namespace         string
	ClusterConfigFile string
	Clientset         *kubernetes.Clientset
}

type SmokeTestRunner struct {
	PubCluster  *ClusterContext
	PrivCluster *ClusterContext
}

func BuildClusterContext(namespace string, configFile string) *ClusterContext {
	cc := &ClusterContext{}
	cc.Namespace = namespace
	cc.ClusterConfigFile = configFile

	config, err := clientcmd.BuildConfigFromFlags("", configFile)
	if err != nil {
		log.Fatal(err.Error())
	}

	cc.Clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err.Error())
	}
	return cc
}

//TODO look for nicer way to execute in golang
func _exec(command string, wait bool) {
	var output []byte
	var err error
	fmt.Println(command)
	cmd := exec.Command("sh", "-c", command)
	if wait {
		output, err = cmd.CombinedOutput()
	} else {
		cmd.Start()
		return
	}
	if err != nil {
		panic(err)
		//os.Stderr.WriteString(err.Error())
	}
	fmt.Println(string(output))
}

func (cc *ClusterContext) exec(main_command string, sub_command string, wait bool) {
	_exec("KUBECONFIG="+cc.ClusterConfigFile+" "+main_command+" "+cc.Namespace+" "+sub_command, wait)
}

func (cc *ClusterContext) skupper_exec(command string) {
	cc.exec("./skupper -n ", command, true)
}

func (cc *ClusterContext) _kubectl_exec(command string, wait bool) {
	cc.exec("kubectl -n ", command, wait)
}

func (cc *ClusterContext) kubectl_exec(command string) {
	cc._kubectl_exec(command, true)
}

func (cc *ClusterContext) kubectl_exec_async(command string) {
	cc._kubectl_exec(command, false)
}

func (cc *ClusterContext) createNamespace() {
	NsSpec := &apiv1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: cc.Namespace}}
	_, err := cc.Clientset.CoreV1().Namespaces().Create(NsSpec)
	if err != nil {
		log.Fatal(err.Error())
	}
}

func (cc *ClusterContext) deleteNamespace() {
	err := cc.Clientset.CoreV1().Namespaces().Delete(cc.Namespace, &metav1.DeleteOptions{})
	if err != nil {
		log.Fatal(err.Error())
	}
}

func (cc *ClusterContext) DescribePods() {

}

func (cc *ClusterContext) GetService(name string, timeout_S time.Duration) *apiv1.Service {
	timeout := time.After(timeout_S * time.Second)
	tick := time.Tick(3 * time.Second)
	for {
		select {
		case <-timeout:
			log.Fatalln("Timed Out Waiting for service.")
		case <-tick:
			service, err := cc.Clientset.CoreV1().Services(cc.Namespace).Get(name, metav1.GetOptions{})
			if err == nil {
				return service
			} else {
				log.Println("Service not ready yet, current pods state: ")
				cc.kubectl_exec("get pods -o wide") //TODO use clientset
			}

		}
	}
}

func (r *SmokeTestRunner) buildForTcpEchoTest(publicConficFile, privateConfigFile string) {
	r.PubCluster = BuildClusterContext("public", publicConficFile)
	r.PrivCluster = BuildClusterContext("private", privateConfigFile)
}

func (r *SmokeTestRunner) _runTcpEchoTest() {
	var publicService *apiv1.Service
	var privateService *apiv1.Service

	//TODO deduplicate
	r.PubCluster.kubectl_exec("get svc")
	r.PrivCluster.kubectl_exec("get svc")

	publicService = r.PubCluster.GetService("tcp-go-echo", minute)
	privateService = r.PrivCluster.GetService("tcp-go-echo", minute)

	fmt.Printf("Public service ClusterIp = %q\n", publicService.Spec.ClusterIP)
	fmt.Printf("Private service ClusterIp = %q\n", privateService.Spec.ClusterIP)

	//sendReceive(publicService.Spec.ClusterIP + ":9090")
	//sendReceive(privateService.Spec.ClusterIP + ":9090")

	r.PubCluster.kubectl_exec_async("port-forward service/tcp-go-echo 9090:9090")
	r.PrivCluster.kubectl_exec_async("port-forward service/tcp-go-echo 9091:9090")
	sendReceive("127.0.0.1:9090")
	sendReceive("127.0.0.1:9091")
}

func (r *SmokeTestRunner) setupTcpEchoTest() {
	r.PubCluster.createNamespace()
	r.PrivCluster.createNamespace()

	publicDeploymentsClient := r.PubCluster.Clientset.AppsV1().Deployments("public")

	fmt.Println("Creating deployment...")
	result, err := publicDeploymentsClient.Create(deployment)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Created deployment %q.\n", result.GetObjectMeta().GetName())

	fmt.Printf("Listing deployments in namespace %q:\n", "public")
	list, err := publicDeploymentsClient.List(metav1.ListOptions{})
	if err != nil {
		log.Fatal(err.Error())
	}
	for _, d := range list.Items {
		fmt.Printf(" * %s (%d replicas)\n", d.Name, *d.Spec.Replicas)
	}

	r.PubCluster.skupper_exec("init --cluster-local")
	r.PubCluster.skupper_exec("expose --port 9090 deployment tcp-go-echo")
	r.PubCluster.skupper_exec("connection-token /tmp/public_secret.yaml")

	r.PrivCluster.skupper_exec("init --cluster-local")
	r.PrivCluster.skupper_exec("connect /tmp/public_secret.yaml")

	r.PubCluster.GetService("tcp-go-echo", 10*minute)
	r.PrivCluster.GetService("tcp-go-echo", 3*minute)
}

func (r *SmokeTestRunner) tearDownTcpEchoTest() {
	//since this is going to run in a spawned ci vm (then destroyed) probably
	//tearDown is not so important
	publicDeploymentsClient := r.PubCluster.Clientset.AppsV1().Deployments("public")
	fmt.Println("Deleting deployment...")
	deletePolicy := metav1.DeletePropagationForeground
	if err := publicDeploymentsClient.Delete("tcp-go-echo", &metav1.DeleteOptions{
		PropagationPolicy: &deletePolicy,
	}); err != nil {
		log.Fatal(err.Error())
	}
	fmt.Println("Deleted deployment.")

	r.PubCluster.skupper_exec("delete")
	r.PrivCluster.skupper_exec("delete")

	//r.deleteNamespaces()??
	r.PubCluster.deleteNamespace()
	r.PrivCluster.deleteNamespace()
}

func (r *SmokeTestRunner) runTcpEchoTest() {
	defer r.tearDownTcpEchoTest()
	r.setupTcpEchoTest()
	r._runTcpEchoTest()
}

func main() {
	testRunner := &SmokeTestRunner{}

	defaultKubeConfig := filepath.Join(homedir.HomeDir(), ".kube", "config")

	pubKubeconfig := flag.String("pubkubeconfig", defaultKubeConfig, "(optional) absolute path to the kubeconfig file")
	privKubeconfig := flag.String("privkubeconfig", defaultKubeConfig, "(optional) absolute path to the kubeconfig file")
	flag.Parse()

	testRunner.buildForTcpEchoTest(*pubKubeconfig, *privKubeconfig)

	log.Printf("using public kubeconfig %v\n", *pubKubeconfig)
	log.Printf("using private kubeconfig %v\n", *privKubeconfig)

	//testRunner._runTcpEchoTest()
	testRunner.runTcpEchoTest()
}
