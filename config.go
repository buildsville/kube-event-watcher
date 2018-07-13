package main

import (
	"errors"
	"flag"
	"github.com/golang/glog"
	"github.com/mitchellh/go-homedir"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"regexp"
)

type Config struct {
	Namespace      string          `yaml:"namespace"`
	WatchEvent     watchEvent      `yaml:"watchEvent"`
	FieldSelectors []fieldSelector `yaml:"fieldSelectors"`
}

type watchEvent struct {
	ADDED    bool `yaml:"ADDED"`
	MODIFIED bool `yaml:"MODIFIED"`
	DELETED  bool `yaml:"DELETED"`
}

type fieldSelector struct {
	Key    string `yaml:"key"`
	Value  string `yaml:"value"`
	Except bool   `yaml:"except"`
}

//configの指定がない場合のdefaultを設けておく
var DefaultConfigPath = func() string {
	if home, err := homedir.Dir(); err == nil {
		return home + "/.kube-event-watcher/config.yaml"
	} else {
		panic(err)
	}
}()

var appConfig = loadConfig()

func configPath() string {
	c := flag.String("config", DefaultConfigPath, "string flag")
	flag.Parse()
	if *c != "" {
		home, err := homedir.Dir()
		if err != nil {
			panic(err)
		}
		r := regexp.MustCompile(`^~`)
		return r.ReplaceAllString(*c, home)
	} else {
		return DefaultConfigPath
	}
}

func loadConfig() []Config {
	buf, err := ioutil.ReadFile(configPath())
	if err != nil {
		panic(err)
	}

	var c []Config
	//yamlに対応するfieldがなければ空の値がstructに入る
	err = yaml.Unmarshal(buf, &c)
	if err != nil {
		panic(err)
	}

	glog.Infof("config loaded: %+v\n", c)
	return c
}

func validateConfig() error {
	if len(appConfig) == 0 {
		return errors.New("config error: set at least one")
	}
	/* 指定なければallにすればよさそうだからとりあえずいらないかな
	for _, c := range appConfig {
	  //require `Kind`
	  if c.Kind == "" {
	    return errors.New("config error: kind is not specified")
	  }
	}
	*/
	return nil
}
