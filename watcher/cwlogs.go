package watcher

import (
	"bytes"
	"flag"
	"regexp"
	"text/template"
	"time"

	"github.com/golang/glog"
	v1 "k8s.io/api/core/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
)

type cwLogConfig struct {
	CWLogging   bool
	CWLogGroup  string
	CWLogStream string
	Template    *template.Template
}

type evPlusAct struct {
	v1.Event
	Action string
}

const (
	defaultCWLogging   = false
	defaultCWLogGroup  = "kube-event-watcher"
	defaultCWLogStream = "event"
)

var (
	globalCWLogging    = flag.Bool("cwLogging", defaultCWLogging, "Whether to logging events to Cloudwatch logs.")
	globalCWLogGroup   = flag.String("cwLogGroup", defaultCWLogGroup, "Loggroup name on logging")
	globalCWLogStream  = flag.String("cwLogStream", defaultCWLogStream, "Logstream name on logging")
	cwlogsTemplateFile = flag.String("cwlogsTemplateFile", "", "Path of Clowdwatch logs template file.")
)

var cwlogDefTpl = `{
    "status":"{{.Type}}",
    "namespace":"{{.ObjectMeta.Namespace}}",
    "objectKind":"{{.InvolvedObject.Kind}}({{if .InvolvedObject.FieldPath}}{{.InvolvedObject.FieldPath}}{{else}}-{{end}})",
    "objectName":"{{.InvolvedObject.Name}}",
    "reason":"{{.Reason}}",
    "message":"{{escapeQuotation .Message}}",
    "count":{{.Count}}
}`

var cwSession = func() *cloudwatchlogs.CloudWatchLogs {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	return cloudwatchlogs.New(sess)
}()

var tplFuncs = map[string]interface{}{
	"escapeQuotation": func(str string) string {
		return regexp.MustCompile(`"`).ReplaceAllString(str, `\"`)
	},
}

func loadCWLogConfig() cwLogConfig {
	te := evPlusAct{
		Event:  v1.Event{},
		Action: "",
	}
	return cwLogConfig{
		CWLogging:   *globalCWLogging,
		CWLogGroup:  *globalCWLogGroup,
		CWLogStream: *globalCWLogStream,
		Template:    loadTemplate(cwlogDefTpl, *cwlogsTemplateFile, tplFuncs, te),
	}
}

// ValidateCWLogs : 指定されたCWLogsのlogGroupとlogStreamが使用可能かどうか
func ValidateCWLogs() error {
	if !*globalCWLogging {
		glog.Infof("disable Cloudwatch logging.\n")
		return nil
	}
	if e := checkLogGroup(*globalCWLogGroup); e != nil {
		return e
	}
	if e := postInitMessage(*globalCWLogGroup, *globalCWLogStream); e != nil {
		return e
	}
	glog.Infof("enable Cloudwatch logging.\n")
	glog.Infof("log group name : %v.\n", *globalCWLogGroup)
	glog.Infof("default log stream name : %v.\n", *globalCWLogStream)
	return nil
}

func postInitMessage(group string, stream string) error {
	event := []*cloudwatchlogs.InputLogEvent{}
	e := &cloudwatchlogs.InputLogEvent{
		Message:   aws.String("application start"),
		Timestamp: aws.Int64(time.Now().Unix() * 1000),
	}
	event = append(event, e)
	if e := tokenAndPutWithRetry(event, group, stream); e != nil {
		return e
	}
	return nil
}

func checkLogGroup(group string) error {
	input := &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String(group),
	}
	if r, e := cwSession.DescribeLogGroups(input); e == nil {
		if len(r.LogGroups) == 0 {
			if e := createLogGroup(group); e != nil {
				return e
			}
		}
	} else {
		return e
	}
	return nil
}

func createLogGroup(group string) error {
	input := &cloudwatchlogs.CreateLogGroupInput{
		LogGroupName: aws.String(group),
	}
	_, err := cwSession.CreateLogGroup(input)
	return err
}

func createStream(group string, stream string) error {
	input := &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(group),
		LogStreamName: aws.String(stream),
	}
	_, err := cwSession.CreateLogStream(input)
	return err
}

//UploadSequenceTokenがなければnilが返るがそれで問題ないっぽい、というかそのほうが便利だった
func token(group string, stream string) (token *string, err error) {
	input := &cloudwatchlogs.DescribeLogStreamsInput{
		LogGroupName:        aws.String(group),
		LogStreamNamePrefix: aws.String(stream),
	}
	x, err := cwSession.DescribeLogStreams(input)
	if err == nil {
		if len(x.LogStreams) == 0 {
			err = createStream(group, stream)
		} else {
			token = x.LogStreams[0].UploadSequenceToken
		}
	}
	return
}

func putEvent(event []*cloudwatchlogs.InputLogEvent, seqtoken *string, group string, stream string) error {
	events := &cloudwatchlogs.PutLogEventsInput{
		LogEvents:     event,
		LogGroupName:  aws.String(group),
		LogStreamName: aws.String(stream),
		SequenceToken: seqtoken,
	}
	//return contains only token `ret["NextSequenceToken"]`
	_, err := cwSession.PutLogEvents(events)
	return err
}

//sometimes scramble with other processes with `NextSequenceToken` and fail
func tokenAndPutWithRetry(event []*cloudwatchlogs.InputLogEvent, group string, stream string) error {
	seqtoken, err := token(group, stream)
	if err != nil {
		return err
	}
	err = putEvent(event, seqtoken, group, stream)
	if err != nil {
		//https://docs.aws.amazon.com/sdk-for-go/api/aws/awserr/#Error
		if aerr, ok := err.(awserr.Error); ok {
			if aerr.Code() == "InvalidSequenceTokenException" {
				glog.Infof("Catch InvalidSequenceTokenException, Retry With Newtoken")
				err = tokenAndPutWithRetry(event, group, stream)
			}
		}
	}
	return err
}

func prepareCWMessage(event v1.Event, action string, tpl *template.Template) (string, int64) {
	pa := evPlusAct{
		Event:  event,
		Action: action,
	}
	var buf bytes.Buffer
	if err := tpl.Execute(&buf, pa); err != nil {
		glog.Errorf("template parse error : ", err)
		return "", time.Now().Unix()
	}
	return buf.String(), event.LastTimestamp.Unix()
}

func escapeQuotation(str string) string {
	return regexp.MustCompile(`"`).ReplaceAllString(str, `\"`)
}

func postEventToCWLogs(obj interface{}, action string, conf cwLogConfig) error {
	if !*globalCWLogging {
		return nil
	}
	cwevent := []*cloudwatchlogs.InputLogEvent{}
	e := &cloudwatchlogs.InputLogEvent{}
	switch aObj := obj.(type) {
	case *v1.Event:
		msg, ts := prepareCWMessage(*aObj, action, conf.Template)
		e.Message = aws.String(msg)
		e.Timestamp = aws.Int64(ts * 1000)
	case string:
		e.Message = aws.String(aObj)
		e.Timestamp = aws.Int64(time.Now().Unix() * 1000)
	default:
		glog.Errorf("Not supported type : %T\n", obj)
		return nil
	}
	cwevent = append(cwevent, e)
	err := tokenAndPutWithRetry(cwevent, conf.CWLogGroup, conf.CWLogStream)
	return err
}
