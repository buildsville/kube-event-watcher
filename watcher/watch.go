package watcher

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/golang/glog"
	"github.com/mitchellh/go-homedir"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/workqueue"
)

const (
	maxRetries            = 5
	defaultKubeconfigPath = "~/.kube/config"
)

var serverStartTime time.Time

type controller struct {
	indexer    cache.Indexer
	queue      workqueue.RateLimitingInterface
	informer   cache.Controller
	channel    string
	logSetting cwLogSetting
}

type event struct {
	key       string
	eventType string
	send      bool
}

var (
	kubeconfig = flag.String("kubeconfig", defaultKubeconfigPath, "Path to kubeconfig file. Generally use ServiceAccount in manifest, so don't need this.")
)

func kubeClient() kubernetes.Interface {
	var ret kubernetes.Interface
	config, err := rest.InClusterConfig()
	if err != nil {
		var kubeconfigPath string
		r := regexp.MustCompile(`^~`)
		home, err := homedir.Dir()
		if err != nil {
			panic(err)
		}
		if os.Getenv("KUBECONFIG") == "" {
			kubeconfigPath = r.ReplaceAllString(*kubeconfig, home)
		} else {
			kubeconfigPath = r.ReplaceAllString(os.Getenv("KUBECONFIG"), home)
		}
		glog.Infoln("use kubeconfig :", kubeconfigPath)
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

func newController(queue workqueue.RateLimitingInterface, indexer cache.Indexer, informer cache.Controller, channel string, logSetting cwLogSetting) *controller {
	return &controller{
		informer:   informer,
		indexer:    indexer,
		queue:      queue,
		channel:    channel,
		logSetting: logSetting,
	}
}

func (c *controller) processNextItem() bool {
	// Wait until there is a new item in the working queue
	ev, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(ev)
	err := c.processItem(ev.(event))
	// Handle the error if something went wrong during the execution of the business logic
	c.handleErr(err, ev)
	return true
}

func (c *controller) processItem(ev event) error {
	obj, _, err := c.indexer.GetByKey(ev.key)
	if err != nil {
		glog.Warningf("Fetching object with key %s from store failed with %v", ev.key, err)
		return err
	}

	if ev.send {
		if ev.eventType != "DELETED" {
			assertedObj, ok := obj.(*v1.Event)
			if !ok {
				glog.Warningf("object with key %s is not *v1.Event", ev.key)
				return nil
			}

			if ev.eventType == "ADDED" { //case "ADDED"
				//起動時に取得する既存のlistは出力させない
				if assertedObj.ObjectMeta.CreationTimestamp.Sub(serverStartTime).Seconds() >= 0 {
					setPromMetrics(assertedObj)
					if e := postEventToSlack(assertedObj, "created", assertedObj.Type, c.channel); e != nil {
						return e
					}
					if e := postEventToCWLogs(assertedObj, "created", c.logSetting); e != nil {
						//cwlogsのエラーはreturnしない（retryしない）
						glog.Errorf("Error send cloudwatch logs : \n", e)
					}
					return nil
				}
			} else { //case "MODIFIED"
				//不定期に起こる謎のupdate(`resourceVersion for the provided watch is too old`)を排除するためlastTimestampから1分未満の時だけpost
				if time.Now().Local().Unix()-assertedObj.LastTimestamp.Unix() < 60 {
					setPromMetrics(assertedObj)
					if e := postEventToSlack(assertedObj, "updated", assertedObj.Type, c.channel); e != nil {
						return e
					}
					if e := postEventToCWLogs(assertedObj, "updated", c.logSetting); e != nil {
						glog.Errorf("Error send cloudwatch logs : \n", e)
					}
					return nil
				}
			}
		} else { //case "DELETED"
			if e := postEventToSlack(fmt.Sprintf("Event %s has been deleted.", ev.key), "deleted", "Danger", c.channel); e != nil {
				return e
			}
			if e := postEventToCWLogs(fmt.Sprintf("Event %s has been deleted.", ev.key), "deleted", c.logSetting); e != nil {
				glog.Errorf("Error send cloudwatch logs : \n", e)
			}
			return nil
		}
	}
	return nil
}

// handleErr checks if an error happened and makes sure we will retry later.
func (c *controller) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		glog.Errorf("Error syncing Event %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	c.queue.Forget(key)
	runtime.HandleError(err)
	glog.Infof("Dropping Event %q out of the queue: %v", key, err)
}

func (c *controller) run(stopCh chan struct{}) {
	defer runtime.HandleCrash()
	defer c.queue.ShutDown()
	glog.Infoln("Starting Event controller")
	serverStartTime = time.Now().Local()

	go c.informer.Run(stopCh)

	// Wait for all involved caches to be synced, before processing items from the queue is started
	if !cache.WaitForCacheSync(stopCh, c.informer.HasSynced) {
		runtime.HandleError(fmt.Errorf("Timed out waiting for caches to sync"))
		return
	}

	go wait.Until(c.runWorker, time.Second, stopCh)

	<-stopCh
	glog.Infoln("Stopping Event controller")
}

func (c *controller) runWorker() {
	for c.processNextItem() {
	}
}

func makeFieldSelector(conf []fieldSelector) fields.Selector {
	if len(conf) == 0 {
		return fields.Everything()
	}
	var selectors []fields.Selector
	for _, s := range conf {
		if s.Type == "exclude" {
			selectors = append(selectors, fields.OneTermNotEqualSelector(s.Key, s.Value))
		} else {
			selectors = append(selectors, fields.OneTermEqualSelector(s.Key, s.Value))
		}
	}
	return fields.AndSelectors(selectors...)
}

// WatchStart : eventをwatchするためのmain function
func WatchStart(appConfig []Config) {
	client := kubeClient()
	for _, c := range appConfig {
		fieldSelector := makeFieldSelector(c.FieldSelectors)
		eventListWatcher := cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "events", c.Namespace, fieldSelector)
		queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		indexer, informer := cache.NewIndexerInformer(eventListWatcher, &v1.Event{}, 0, resourceEventHandlerFuncs(queue, c.WatchEvent), cache.Indexers{})
		var channel string
		if c.Channel == "" {
			channel = slackConf.Channel
		} else {
			channel = c.Channel
		}
		logSetting := loadGlobalCWLogSetting()
		if c.LogStream != "" {
			logSetting.CWLogStream = c.LogStream
		}
		controller := newController(queue, indexer, informer, channel, logSetting)
		stop := make(chan struct{})
		defer close(stop)
		go controller.run(stop)
	}

	sigterm := make(chan os.Signal, 1)
	signal.Notify(sigterm, syscall.SIGTERM)
	signal.Notify(sigterm, syscall.SIGINT)
	<-sigterm
}

func resourceEventHandlerFuncs(queue workqueue.RateLimitingInterface, we watchEvent) cache.ResourceEventHandlerFuncs {
	var ev event
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