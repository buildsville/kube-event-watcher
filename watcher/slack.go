package watcher

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"text/template"

	"github.com/golang/glog"
	"github.com/nlopes/slack"
	v1 "k8s.io/api/core/v1"
)

const (
	defaultNotifySlack = true
)

var (
	notifySlack       = flag.Bool("notifySlack", defaultNotifySlack, "Whether to notify events to Slack.")
	slackTemplateFile = flag.String("slackTemplateFile", "", "Path of Slack template file.")
)

type slackConfig struct {
	Token    string
	Channel  string
	Template *template.Template
}

var slackColors = map[string]string{
	"Normal":  "good",
	"Warning": "warning",
	"Danger":  "danger",
}

var slackConfBase = slackConfig{
	Token:   os.Getenv("SLACK_TOKEN"),
	Channel: os.Getenv("SLACK_CHANNEL"),
}

var slackDefTpl = `namespace: {{.ObjectMeta.Namespace}}
objectKind: {{.InvolvedObject.Kind}} ({{if .InvolvedObject.FieldPath}}{{.InvolvedObject.FieldPath}}{{else}}-{{end}})
objectName: {{.InvolvedObject.Name}}
reason: {{.Reason}}
message: {{.Message}}
count: {{.Count}}`

func loadSlackConfig() slackConfig {
	c := slackConfBase
	funcs := map[string]interface{}{}
	c.Template = loadTemplate(slackDefTpl, *slackTemplateFile, funcs, v1.Event{})
	return c
}

// ValidateSlack : 指定されたslackのチャンネルが使用可能かどうか。実際postする以外にprivateチャンネルの存在確認する方法はないかな…
func ValidateSlack() error {
	if !*notifySlack {
		glog.Infof("disable notify Slack.\n")
		return nil
	}
	if slackConfBase.Token == "" || slackConfBase.Channel == "" {
		return errors.New("slack error: token or channel is empty")
	}
	glog.Infof("default slack channel: %v\n", slackConfBase.Channel)
	api := slack.New(slackConfBase.Token)
	title := "kube-event-watcher"
	text := "application start"
	params := prepareParams(title, text, "good")
	if _, _, e := api.PostMessage(slackConfBase.Channel, "", params); e != nil {
		return e
	}
	return nil
}

func prepareParams(title string, text string, color string) slack.PostMessageParameters {
	params := slack.PostMessageParameters{
		AsUser: true,
	}
	attachment := slack.Attachment{
		Color: color,
		Fields: []slack.AttachmentField{
			slack.AttachmentField{
				Title: title,
				Value: text,
			},
		},
	}
	params.Attachments = []slack.Attachment{attachment}
	return params
}

func prepareSlackMessage(event v1.Event, tpl *template.Template) string {
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, event); err != nil {
		glog.Errorf("template parse error : ", err)
		return ""
	}
	return buf.String()
}

func postEventToSlack(obj interface{}, action string, status string, conf slackConfig) error {
	if !*notifySlack {
		return nil
	}
	api := slack.New(conf.Token)
	title := "kubernetes event : " + action
	color, ok := slackColors[status]
	if !ok {
		color = "danger"
	}
	var message string
	switch e := obj.(type) {
	case *v1.Event:
		message = prepareSlackMessage(*e, conf.Template)
	case string:
		message = e
	default:
		glog.Errorf("Not supported type : %T\n", obj)
		return nil
	}
	params := prepareParams(title, message, color)
	_, _, err := api.PostMessage(conf.Channel, "", params)
	if err != nil {
		if err.Error() == "channel_not_found" {
			glog.Infof("error : channel %v not found, send message to default channel", conf.Channel)
			_, _, err = api.PostMessage(conf.Channel, "", params)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
