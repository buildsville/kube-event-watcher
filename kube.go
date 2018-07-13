package main

import (
	"fmt"
	"github.com/mitchellh/go-homedir"
	"os"
	"os/signal"
	"syscall"
	"time"
	//"github.com/golang/glog"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

const maxRetries = 5

var serverStartTime time.Time

type Controller struct {
	indexer  cache.Indexer
	queue    workqueue.RateLimitingInterface
	informer cache.Controller
}

type Event struct {
	key       string
	eventType string
	send      bool
}

func kubeClient() kubernetes.Interface {
	var ret kubernetes.Interface
	config, err := rest.InClusterConfig()
	if err != nil {
		var kubeconfigPath string
		if os.Getenv("KUBECONFIG") == "" {
			home, err := homedir.Dir()
			if err != nil {
				panic(err)
			}
			kubeconfigPath = home + "/.kube/config"
		} else {
			kubeconfigPath = os.Getenv("KUBECONFIG")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
		if err != nil {
			panic(err)
		}
	}
	ret, err = kubernetes.NewForConfig(config)
	if err != nil {
		panic(err)
	}
	return ret
}

func NewController(queue workqueue.RateLimitingInterface, indexer cache.Indexer, informer cache.Controller) *Controller {
	return &Controller{
		informer: informer,
		indexer:  indexer,
		queue:    queue,
	}
}

func (c *Controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	ev, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(ev)
	err := c.processItem(ev.(Event))
	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, ev)
	return true
}

func (c *Controller) processItem(ev Event) error {
	obj, _, err := c.indexer.GetByKey(ev.key)
	if err != nil {
		fmt.Printf("Fetching object with key %s from store failed with %v", ev.key, err)
		return err
	}

	message := prepareMessage(obj, ev)

	switch ev.eventType {
	case "ADDED":
		objectMeta := obj.(*v1.Event).ObjectMeta
		//起動時に取得する既存のlistは出力させない
		if ev.send && objectMeta.CreationTimestamp.Sub(serverStartTime).Seconds() > 0 {
			setPromMetrics(obj)
			err := postEventToSlack(message, "created", obj.(*v1.Event).Type)
			if err != nil {
				return err
			}
			return nil
		}
	case "MODIFIED":
		assertedObj := obj.(*v1.Event)
		//不定期に起こる謎のupdateを排除するためlastTimestampから1分未満の時だけpost
		//ここのSubを逆にすると型で怒られる（よくわからん）
		if ev.send && assertedObj.LastTimestamp.Sub(time.Now().Local()).Seconds() > -60 {
			setPromMetrics(obj)
			err := postEventToSlack(message, "updated", obj.(*v1.Event).Type)
			if err != nil {
				return err
			}
			return nil
		}
	case "DELETED":
		if ev.send {
			err := postEventToSlack(message, "deleted", "Danger")
			if err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *Controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		fmt.Printf("Error syncing Event %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	runtime.HandleError(err)
	fmt.Printf("Dropping Event %q out of the queue: %v", key, err)
}

func (c *Controller) Run(stopCh chan struct{}) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()
	fmt.Println("Starting Event controller")
	serverStartTime = time.Now().Local()

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	go wait.Until(c.runWorker, time.Second, stopCh)

	<-stopCh
	fmt.Println("Stopping Event controller")
}

func (c *Controller) runWorker() {
	for c.processNextItem() {
	}
}

func makeFieldSelector(conf []fieldSelector) fields.Selector {
	if len(conf) == 0 {
		return fields.Everything()
	}
	var selectors []fields.Selector
	for _, s := range conf {
		if s.Except {
			selectors = append(selectors, fields.OneTermNotEqualSelector(s.Key, s.Value))
		} else {
			selectors = append(selectors, fields.OneTermEqualSelector(s.Key, s.Value))
		}
	}
	return fields.AndSelectors(selectors...)
}

func watchStart() {
	for _, c := range appConfig {
		client := kubeClient()
		fieldSelector := makeFieldSelector(c.FieldSelectors)
		eventListWatcher := cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "events", c.Namespace, fieldSelector)
		queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		indexer, informer := cache.NewIndexerInformer(eventListWatcher, &v1.Event{}, 0, resourceEventHandlerFuncs(queue, c.WatchEvent), cache.Indexers{})
		controller := NewController(queue, indexer, informer)
		stop := make(chan struct{})
		defer close(stop)
		go controller.Run(stop)
	}

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	signal.Notify(sigterm, syscall.SIGINT)
	<-sigterm
}

func resourceEventHandlerFuncs(queue workqueue.RateLimitingInterface, we watchEvent) cache.ResourceEventHandlerFuncs {
	var ev Event
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(obj)
			ev.key = key
			ev.eventType = "ADDED"
			ev.send = we.ADDED
			if err == nil {
				queue.Add(ev)
			}
		},
		UpdateFunc: func(old, new interface{}) {
			key, err := cache.MetaNamespaceKeyFunc(old)
			ev.key = key
			ev.eventType = "MODIFIED"
			ev.send = we.MODIFIED
			if err == nil {
				queue.Add(ev)
			}
		},
		DeleteFunc: func(obj interface{}) {
			// IndexerInformer uses a delta queue, therefore for deletes we have to use this
			// key function.
			key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
			ev.key = key
			ev.eventType = "DELETED"
			ev.send = we.DELETED
			if err == nil {
				queue.Add(ev)
			}
		},
	}
}

func prepareMessage(obj interface{}, ev Event) string {
	if ev.eventType == "DELETED" {
		return fmt.Sprintf("Event %s has been deleted.", ev.key)
	} else {
		assertedObj := obj.(*v1.Event)
		if assertedObj.InvolvedObject.FieldPath == "" {
			assertedObj.InvolvedObject.FieldPath = "-"
		}
		return fmt.Sprintf("namespace: %s\nobjectKind: %s (%s)\nobjectName: %s\nreason: %s\nmessage: %s\ncount: %d",
			assertedObj.ObjectMeta.Namespace,
			assertedObj.InvolvedObject.Kind,
			assertedObj.InvolvedObject.FieldPath,
			assertedObj.InvolvedObject.Name,
			assertedObj.Reason,
			assertedObj.Message,
			assertedObj.Count,
		)
	}
}
