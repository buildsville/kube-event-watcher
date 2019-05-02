package watcher

import (
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"regexp"
	"text/template"

	"github.com/golang/glog"
	"github.com/mitchellh/go-homedir"
	"gopkg.in/yaml.v2"
)

// Config はファイルで読み込む設定の型
type Config struct {
	Namespace      string          `yaml:"namespace"`
	WatchEvent     watchEvent      `yaml:"watchEvent"`
	FieldSelectors []fieldSelector `yaml:"fieldSelectors"`
	Channel        string          `yaml:"channel"`
	LogStream      string          `yaml:"logStream"`
}

type watchEvent struct {
	ADDED    bool `yaml:"ADDED"`
	MODIFIED bool `yaml:"MODIFIED"`
	DELETED  bool `yaml:"DELETED"`
}

type fieldSelector struct {
	Key   string `yaml:"key"`
	Value string `yaml:"value"`
	Type  string `yaml:"type"`
}

//configの指定がない場合のdefaultを設けておく
const (
	defaultConfigPath = "~/.kube-event-watcher/config.yaml"
)

var (
	confPath = flag.String("config", defaultConfigPath, "Path to config file.")
)

func configPath() string {
	home, err := homedir.Dir()
	if err != nil {
		panic(err)
	}
	r := regexp.MustCompile(`^~`)
	return r.ReplaceAllString(*confPath, home)
}

// LoadConfig :yamlファイルを読み込む
func LoadConfig() ([]Config, error) {
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

func loadTemplate(dt string, fp string, tf map[string]interface{}, td interface{}) *template.Template {
	var e error
	var lt *template.Template
	var o os.FileInfo
	if fp != "" {
		glog.Infof("load template file %v", fp)
		o, e = os.Stat(fp)
		if e == nil {
			lt, e = template.New(o.Name()).Funcs(tf).ParseFiles(fp)
			if e == nil {
				e = lt.Execute(ioutil.Discard, td)
				if e == nil {
					return lt
				}
			}
		}
	}
	if e != nil {
		glog.Errorf("load default template since load error : %v", e)
	}
	return template.Must(template.New("").Funcs(tf).Parse(dt))
}
