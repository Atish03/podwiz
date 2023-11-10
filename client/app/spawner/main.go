package spawner

import (
	"fmt"
	"os"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/client-go/tools/portforward"
    "k8s.io/client-go/transport/spdy"
	"k8s.io/client-go/rest"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
	"path/filepath"
	"flag"
	"context"
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"time"
    "math/rand"
	"github.com/Atish03/podwiz/app/builder"
)

type Client struct {
	config *rest.Config
	clientset *kubernetes.Clientset
}

type ShellPod struct {
	client Client
	PodName string
	pod *apiv1.Pod
}

type User struct {
	Username string
	Password string
	Shell *ShellPod
	Forwarder *portforward.PortForwarder
	Port uint16
}

type Scheduler struct {
	Name string
	period int
	StartTime int64
	EndTime int64
	User *User
}

func init() {
    rand.Seed(time.Now().UnixNano())
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
var lowerCase = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

func randSeq(n int) string {
    b := make([]rune, n)
    for i := range b {
        b[i] = letters[rand.Intn(len(letters))]
    }
    return string(b)
}

func randName(n int) string {
    b := make([]rune, n)
    for i := range b {
        b[i] = lowerCase[rand.Intn(len(lowerCase))]
    }
    return string(b)
}

func (user *User) Delete() {
	user.Forwarder.Close()

	err := user.Shell.client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Delete(context.TODO(), user.Shell.PodName, metav1.DeleteOptions{})
	if err != nil {
		panic(err)
	}

	user.Shell = nil
	user.Forwarder = nil
}

func (client *Client) CreateUser(username string, podName string, podFile string, image string) User {
	var password string = randSeq(15)
	randomShellName := podName + randName(6)

	user := User {
		Username: username,
		Password: password,
	}

	shell := ShellPod {
		client: *client,
		PodName: randomShellName,
	}

	pod := shell.startPod(podFile, password, randomShellName, image)
	shell.pod = pod
	forwarder := shell.forwardPod()

	user.Shell = &shell
	user.Forwarder = forwarder

	ports, err := user.Forwarder.GetPorts()
	if err != nil {
		panic(err)
	}

	user.Port = ports[0].Local

	return user
}

func GetClient() Client {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err)
	}
	
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}

	client := Client {
		config: config,
		clientset: clientset,
	}

	return client
}

func (shell *ShellPod) startPod(path string, password string, name string, image string) *apiv1.Pod {
	client := shell.client

	dat, err := os.ReadFile(path + "/pod.yaml")
	if err != nil {
		panic(err)
	}

	dat = bytes.ReplaceAll(dat, []byte("%password%"), []byte(password))
	dat = bytes.ReplaceAll(dat, []byte("%name%"), []byte(name))
	dat = bytes.ReplaceAll(dat, []byte("%image%"), []byte(image))
	
	pod := &apiv1.Pod{}
	err = yaml.Unmarshal(dat, pod)
	if err != nil {
		panic(err)
	}

	pod, err = client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Create(context.TODO(), pod, metav1.CreateOptions{})
	if err != nil {
		panic(err)
	}

	for {
		createdPod, err := client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Get(context.TODO(), shell.PodName, metav1.GetOptions{})
		if err != nil {
			panic(err)
		}

		if len(createdPod.Status.ContainerStatuses) > 0 {
			if (createdPod.Status.ContainerStatuses[0].State.Waiting != nil && createdPod.Status.ContainerStatuses[0].State.Waiting.Reason == "ErrImageNeverPull") {
				fmt.Printf("Cannot find %s, building from Dockerfile", image)
				err := client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Delete(context.TODO(), shell.PodName, metav1.DeleteOptions{})
				if err != nil {
					panic(err)
				}

				err = builder.Build(path, image)
				if err != nil {
					panic(err)
				}

				pod = &apiv1.Pod{}
				err = yaml.Unmarshal(dat, pod)
				if err != nil {
					panic(err)
				}

				pod, err = client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Create(context.TODO(), pod, metav1.CreateOptions{})
				if err != nil {
					panic(err)
				}
			}
		}

		if createdPod.Status.Phase == "Running" {
			break
		}
	}

	return pod
}

func (shell *ShellPod) forwardPod() *portforward.PortForwarder {
	client := shell.client

	roundTripper, upgrader, err := spdy.RoundTripperFor(client.config)
	if err != nil {
		panic(err)
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", shell.pod.Namespace, shell.PodName)
	hostIP := strings.TrimLeft(client.config.Host, "htps:/")
	serverURL := url.URL{Scheme: "https", Path: path, Host: hostIP}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, &serverURL)


	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	forwarder, err := portforward.New(dialer, []string{":22"}, stopChan, readyChan, out, errOut)
	if err != nil {
		panic(err)
	}
	
	go forwarder.ForwardPorts()

	for range readyChan {
	}

	return forwarder
}

func New(time int, name string) *Scheduler {
	s := Scheduler {
		Name: name,
		period: time,
	}
	return &s
}

func (s *Scheduler) Start(toKill *User) {
	s.User = toKill
	s.StartTime = time.Now().Unix()
	s.EndTime = s.StartTime + int64(s.period)

	for s.period != 0 {
		timeToSleep := s.period
		s.period -= timeToSleep
		time.Sleep(time.Duration(timeToSleep) * time.Second)
	}

	fmt.Println("Deleting", s.Name)
	s.User.Delete()
	s.User = nil
	*s = Scheduler{}
	return
}

func (s *Scheduler) AddTime(time int) {
	s.period += time
	s.EndTime += int64(time)
}