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

type ClientError struct {
	Err string
}

type InternalError struct {
	Err string
}

func (e *ClientError) Error() string {
	return fmt.Sprintf("%v: client error", e.Err)
}

func (e *InternalError) Error() string {
	return fmt.Sprintf("parse %v: internal error", e.Err)
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

func (user *User) Delete() error {
	user.Forwarder.Close()

	err := user.Shell.client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Delete(context.TODO(), user.Shell.PodName, metav1.DeleteOptions{})
	if err != nil {
		return &InternalError {
			err.Error(),
		}
	}

	user.Shell = nil
	user.Forwarder = nil

	return nil
}

func (client *Client) CreateUser(username string, podName string, podFile string, image string) (User, error) {
	var password string = randSeq(15)
	randomShellName := podName + randName(12)

	user := User {
		Username: username,
		Password: password,
	}

	shell := ShellPod {
		client: *client,
		PodName: randomShellName,
	}

	pod, err := shell.startPod(podFile, password, randomShellName, image)
	if err != nil {
		return User{}, err
	}

	shell.pod = pod
	forwarder, err := shell.forwardPod()
	if err != nil {
		return User{}, err
	}

	user.Shell = &shell
	user.Forwarder = forwarder

	ports, err := user.Forwarder.GetPorts()
	if err != nil {
		return User{}, &InternalError {
			err.Error(),
		}
	}

	user.Port = ports[0].Local

	return user, nil
}

func GetClient() (Client, error) {
	var kubeconfig *string
	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		return Client{}, &ClientError {
			"k8s is not correctly configured, make sure that .kube exists in home dierctory",
		}
	}
	
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return Client{}, &InternalError {
			err.Error(),
		}
	}

	client := Client {
		config: config,
		clientset: clientset,
	}

	return client, nil
}

func (shell *ShellPod) startPod(path string, password string, name string, image string) (*apiv1.Pod, error) {
	client := shell.client

	dat, err := os.ReadFile(path + "/pod.yaml")
	if err != nil {
		return nil, &ClientError {
			err.Error(),
		}
	}

	dat = bytes.ReplaceAll(dat, []byte("%password%"), []byte(password))
	dat = bytes.ReplaceAll(dat, []byte("%name%"), []byte(name))
	dat = bytes.ReplaceAll(dat, []byte("%image%"), []byte(image))
	
	pod := &apiv1.Pod{}
	err = yaml.Unmarshal(dat, pod)
	if err != nil {
		return nil, &ClientError {
			"Cannot parse yaml file, please check the syntax\n" + err.Error(),
		}
	}

	imgExists, err := builder.ImageExists(image)
	if err != nil {
		return nil, err
	}

	if imgExists {
		pod, err = client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Create(context.TODO(), pod, metav1.CreateOptions{})
		if err != nil {
			return nil, &InternalError {
				err.Error(),
			}
		}
	} else {
		err = builder.Build(path, image)
		if err != nil {
			panic(err)
		}

		pod, err = client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Create(context.TODO(), pod, metav1.CreateOptions{})
		if err != nil {
			panic(err)
		}
	}

	for {
		createdPod, err := client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Get(context.TODO(), shell.PodName, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		if len(createdPod.Status.ContainerStatuses) > 0 {
			if (createdPod.Status.ContainerStatuses[0].State.Waiting != nil && createdPod.Status.ContainerStatuses[0].State.Waiting.Reason == "ErrImageNeverPull") {
				client.clientset.CoreV1().Pods(apiv1.NamespaceDefault).Delete(context.TODO(), shell.PodName, metav1.DeleteOptions{})
				return nil, &ClientError {
					fmt.Sprintf("Cannot find %s, make sure shell is pointing to docker-daemon of k8s", image),
				}
			}
		}

		if createdPod.Status.Phase == "Running" {
			break
		}
	}

	return pod, nil
}

func (shell *ShellPod) forwardPod() (*portforward.PortForwarder, error) {
	client := shell.client

	roundTripper, upgrader, err := spdy.RoundTripperFor(client.config)
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("/api/v1/namespaces/%s/pods/%s/portforward", shell.pod.Namespace, shell.PodName)
	hostIP := strings.TrimLeft(client.config.Host, "htps:/")
	serverURL := url.URL{Scheme: "https", Path: path, Host: hostIP}

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, &serverURL)


	stopChan, readyChan := make(chan struct{}, 1), make(chan struct{}, 1)
	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	forwarder, err := portforward.New(dialer, []string{":22"}, stopChan, readyChan, out, errOut)
	if err != nil {
		return nil, err
	}
	
	go forwarder.ForwardPorts()

	for range readyChan {
	}

	return forwarder, nil
}

func New(time int, name string) *Scheduler {
	s := Scheduler {
		Name: name,
		period: time,
	}
	return &s
}

func (s *Scheduler) Start(toKill *User) error {
	s.User = toKill
	s.StartTime = time.Now().Unix()
	s.EndTime = s.StartTime + int64(s.period)

	for s.period != 0 {
		timeToSleep := s.period
		s.period -= timeToSleep
		time.Sleep(time.Duration(timeToSleep) * time.Second)
	}

	fmt.Println("Deleting", s.Name)
	err := s.User.Delete()
	if err != nil {
		return err
	}

	s.User = nil
	*s = Scheduler{}
	return nil
}

func (s *Scheduler) AddTime(time int) {
	s.period += time
	s.EndTime += int64(time)
}