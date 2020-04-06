package watcher

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"regexp"
	"strings"
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

type controller struct {
	indexer     cache.Indexer
	queue       workqueue.RateLimitingInterface
	informer    cache.Controller
	slackConf   slackConfig
	logConf     cwLogConfig
	extraFilter extraFilter
	startTime   time.Time
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

func newController(queue workqueue.RateLimitingInterface, indexer cache.Indexer, informer cache.Controller, slackConfig slackConfig, logConfig cwLogConfig, extraFilter extraFilter, startTime time.Time) *controller {
	return &controller{
		informer:    informer,
		indexer:     indexer,
		queue:       queue,
		slackConf:   slackConfig,
		logConf:     logConfig,
		extraFilter: extraFilter,
		startTime:   startTime,
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

			//起動時に取得する既存のlistはskip
			if assertedObj.ObjectMeta.CreationTimestamp.Sub(c.startTime).Seconds() < 0 {
				return nil
			}

			//不定期に起こる謎のupdate(`resourceVersion for the provided watch is too old`)を排除するためlastTimestampから60秒以上はskip
			if time.Now().Local().Unix()-assertedObj.LastTimestamp.Unix() > 60 {
				return nil
			}

			if exFiltering(assertedObj, c.extraFilter) {
				if glog.V(1) {
					glog.Infof("Filtered by extra filters, %s", ev.key)
				}
				return nil
			}
			if glog.V(1) {
				glog.Infof("Send notify, %s", ev.key)
			}

			switch ev.eventType {
			case "ADDED":
				setPromMetrics(assertedObj)
				if e := postEventToSlack(assertedObj, "created", assertedObj.Type, c.slackConf); e != nil {
					return e
				}
				if e := postEventToCWLogs(assertedObj, "created", c.logConf); e != nil {
					//cwlogsのエラーはreturnしない（retryしない）
					glog.Errorf("Error send cloudwatch logs : %s \n", e)
				}
				return nil
			case "MODIFIED":
				setPromMetrics(assertedObj)
				if e := postEventToSlack(assertedObj, "updated", assertedObj.Type, c.slackConf); e != nil {
					return e
				}
				if e := postEventToCWLogs(assertedObj, "updated", c.logConf); e != nil {
					glog.Errorf("Error send cloudwatch logs : %s \n", e)
				}
				return nil
			default:
				return nil
			}
		}
		//case "DELETED"
		if e := postEventToSlack(fmt.Sprintf("Event %s has been deleted.", ev.key), "deleted", "Danger", c.slackConf); e != nil {
			return e
		}
		if e := postEventToCWLogs(fmt.Sprintf("Event %s has been deleted.", ev.key), "deleted", c.logConf); e != nil {
			glog.Errorf("Error send cloudwatch logs : %s \n", e)
		}
		return nil
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
	const (
		toInclude = "include"
		toExclude = "exclude"
	)
	if len(conf) == 0 {
		return fields.Everything()
	}
	var selectors []fields.Selector
	for _, s := range conf {
		switch s.Type {
		case toExclude:
			selectors = append(selectors, fields.OneTermNotEqualSelector(s.Key, s.Value))
		case toInclude:
			selectors = append(selectors, fields.OneTermEqualSelector(s.Key, s.Value))
		default:
			continue
		}
	}
	return fields.AndSelectors(selectors...)
}

// WatchStart : eventをwatchするためのmain function
func WatchStart(appConfig []Config) {
	client := kubeClient()
	for _, cf := range appConfig {
		fieldSelector := makeFieldSelector(cf.FieldSelectors)
		eventListWatcher := cache.NewListWatchFromClient(client.CoreV1().RESTClient(), "events", cf.Namespace, fieldSelector)
		queue := workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter())
		indexer, informer := cache.NewIndexerInformer(eventListWatcher, &v1.Event{}, 0, resourceEventHandlerFuncs(queue, cf.WatchEvent), cache.Indexers{})
		sc := loadSlackConfig()
		if cf.Channel != "" {
			sc.Channel = cf.Channel
		}
		lc := loadCWLogConfig()
		if cf.LogStream != "" {
			lc.CWLogStream = cf.LogStream
		}
		st := time.Now().Local()
		ef := cf.ExtraFilter

		controller := newController(queue, indexer, informer, sc, lc, ef, st)
		stop := make(chan struct{})
		defer close(stop)
		go controller.run(stop)
	}

	defer postExitMsg()
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

// return trueで通知がスキップされる
func exFiltering(event *v1.Event, exFilter extraFilter) bool {
	if len(exFilter.Filters) == 0 {
		return false
	}
	andCondCnt := 0
	andMatchCnt := 0
	for _, f := range exFilter.Filters {
		if f.Condition == "and" {
			andCondCnt++
		}
	}
	const (
		toKeep = "keep"
		toDrop = "drop"
	)
	for _, f := range exFilter.Filters {
		v := reflect.ValueOf(event).Elem()
		for _, k := range strings.Split(f.Key, ".") {
			v = v.FieldByName(k)
			if !v.IsValid() {
				break
			}
			if v.Kind() == reflect.Struct {
				continue
			}
			if v.Kind() != reflect.String {
				break
			}
			if matchString(f.Value, v.String()) {
				switch exFilter.Type {
				case toKeep:
					if f.Condition != "and" {
						return false
					}
					andMatchCnt++
					if andMatchCnt == andCondCnt {
						return false
					}
					continue
				case toDrop:
					if f.Condition != "and" {
						return true
					}
					andMatchCnt++
					if andMatchCnt == andCondCnt {
						return true
					}
					continue
				default:
					break
				}
			}
		}
	}
	// マッチしなかった時の分岐
	switch exFilter.Type {
	case toKeep:
		return true
	case toDrop:
		return false
	default:
		return false
	}
}

func matchString(pattern string, target string) bool {
	if strings.HasPrefix(pattern, "/") && strings.HasSuffix(pattern, "/") {
		ptn := strings.TrimSuffix(strings.TrimPrefix(pattern, "/"), "/")
		glog.Infoln("use regexp match")
		match, err := regexp.MatchString(ptn, target)
		if match {
			return true
		}
		if err != nil {
			glog.Errorf("Error regexp.MatchString : %s\n", err)
		}
	} else {
		glog.Infoln("use string match")
		if strings.Contains(target, pattern) {
			return true
		}
	}
	return false
}
