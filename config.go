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
	Channel        string          `yaml:"channel"`
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
const DefaultConfigPath = "~/.kube-event-watcher/config.yaml"

var confPath = flag.String("config", DefaultConfigPath, "string flag")

func configPath() string {
	home, err := homedir.Dir()
	if err != nil {
		panic(err)
	}
	r := regexp.MustCompile(`^~`)
	return r.ReplaceAllString(*confPath, home)
}

func loadConfig() ([]Config, error) {
	var c []Config
	buf, err := ioutil.ReadFile(configPath())
	if err != nil {
		return c, err
	}

	//yamlに対応するfieldがなければ空の値がstructに入る
	err = yaml.Unmarshal(buf, &c)
	if err != nil {
		return c, err
	}

	err = validateConfig(c)

	glog.Infof("config loaded: %+v\n", c)
	return c, err
}

func validateConfig(conf []Config) error {
	// 指定なければデフォルト値が入ってとりあえず動くから一旦これで
	if len(conf) == 0 {
		return errors.New("config error: set at least one")
	}
	return nil
}
