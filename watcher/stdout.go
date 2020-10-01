package watcher

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"
)

const (
	defaultPutStout = false
)

var (
	putStdout = flag.Bool("putStdout", defaultPutStout, "Whether to output events to stdout.")
)

func putEventToStdout(obj interface{}) error {
	if !*putStdout {
		return nil
	}

	switch e := obj.(type) {
	case *v1.Event:
		if msgBytes, err := json.Marshal(e); err == nil {
			fmt.Fprintln(os.Stdout, string(msgBytes))
		} else {
			glog.Warningf("Failed to make json string: %v", err)
		}
	default:
		glog.Errorf("Not supported type : %T\n", obj)
		return nil
	}

	return nil
}
