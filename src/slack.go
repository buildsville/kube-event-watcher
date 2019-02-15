package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/golang/glog"
	"github.com/nlopes/slack"
	"k8s.io/api/core/v1"
	"os"
)

const (
	defaultNotifySlack = true
)

var (
	notifySlack = flag.Bool("notifySlack", defaultNotifySlack, "Whether to notify events to Slack.")
)

type SlackConf struct {
	Token   string
	Channel string
}

var slackColors = map[string]string{
	"Normal":  "good",
	"Warning": "warning",
	"Danger":  "danger",
}

var slackConf = func() SlackConf {
	var s SlackConf
	s.Token = os.Getenv("SLACK_TOKEN")
	s.Channel = os.Getenv("SLACK_CHANNEL")
	return s
}()

//実際postする以外にprivateチャンネルの存在確認する方法は…
func validateSlack() error {
	if !*notifySlack {
		glog.Infof("disable notify Slack.\n")
		return nil
	}
	if slackConf.Token == "" || slackConf.Channel == "" {
		return errors.New("slack error: token or channel is empty")
	}
	glog.Infof("default slack channel: %v\n", slackConf.Channel)
	api := slack.New(slackConf.Token)
	title := "kube-event-watcher"
	text := "application start"
	params := prepareParams(title, text, "good")
	if _, _, e := api.PostMessage(slackConf.Channel, "", params); e != nil {
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

func prepareSlackMessage(event *v1.Event) string {
	var fieldPath string
	if event.InvolvedObject.FieldPath == "" {
		fieldPath = "-"
	} else {
		fieldPath = event.InvolvedObject.FieldPath
	}
	return fmt.Sprintf("namespace: %s\nobjectKind: %s (%s)\nobjectName: %s\nreason: %s\nmessage: %s\ncount: %d",
		event.ObjectMeta.Namespace,
		event.InvolvedObject.Kind,
		fieldPath,
		event.InvolvedObject.Name,
		event.Reason,
		event.Message,
		event.Count,
	)
}

func postEventToSlack(obj interface{}, action string, status string, channel string) error {
	if !*notifySlack {
		return nil
	}
	api := slack.New(slackConf.Token)
	title := "kubernetes event : " + action
	color, ok := slackColors[status]
	if !ok {
		color = "danger"
	}
	var message string
	switch e := obj.(type) {
	case *v1.Event:
		message = prepareSlackMessage(e)
	case string:
		message = e
	default:
		glog.Errorf("Not supported type : %T\n", obj)
		return nil
	}
	params := prepareParams(title, message, color)
	_, _, err := api.PostMessage(channel, "", params)
	if err != nil {
		if err.Error() == "channel_not_found" {
			glog.Infof("error : channel %v not found, send message to default channel", channel)
			_, _, err = api.PostMessage(slackConf.Channel, "", params)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
