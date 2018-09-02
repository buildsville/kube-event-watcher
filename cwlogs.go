package main

import (
	"flag"
	"github.com/golang/glog"
	"regexp"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudwatchlogs"
	"k8s.io/api/core/v1"
)

type JsonMap map[string]interface{}

type cwLogSetting struct {
	CWLogging   bool
	CWLogGroup  string
	CWLogStream string
}

const (
	defaultCWLogging   = false
	defaultCWLogGroup  = "kube-event-watcher"
	defaultCWLogStream = "event"
)

var (
	globalCWLogging   = flag.Bool("cwLogging", defaultCWLogging, "Logging events to Cloudwatch logs.")
	globalCWLogGroup  = flag.String("cwLogGroup", defaultCWLogGroup, "Loggroup name on logging")
	globalCWLogStream = flag.String("cwLogStream", defaultCWLogStream, "Logstream name on logging")
)

var globalCWLogSetting = cwLogSetting{
	CWLogging:   *globalCWLogging,
	CWLogGroup:  *globalCWLogGroup,
	CWLogStream: *globalCWLogStream,
}

var cwSession = func() *cloudwatchlogs.CloudWatchLogs {
	sess := session.Must(session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
	}))
	return cloudwatchlogs.New(sess)
}()

func validateCWLogs() error {
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

func prepareCWMessage(event *v1.Event, action string) (string, int64) {
	var fieldPath string
	if event.InvolvedObject.FieldPath == "" {
		fieldPath = "-"
	} else {
		fieldPath = event.InvolvedObject.FieldPath
	}
	ret := `{"action":"` + action + `",
    "status":"` + event.Type + `",
    "namespace":"` + event.ObjectMeta.Namespace + `",
    "objectKind":"` + event.InvolvedObject.Kind + `(` + fieldPath + `)",
    "objectName":"` + event.InvolvedObject.Name + `",
    "reason":"` + event.Reason + `",
    "message":"` + escapeQuotation(event.Message) + `",
    "count":` + strconv.Itoa(int(event.Count)) + `
    }`
	return regexp.MustCompile("[\n\t]").ReplaceAllString(ret, ""), event.LastTimestamp.Unix()
}

func escapeQuotation(str string) string {
	return regexp.MustCompile(`"`).ReplaceAllString(str, `\"`)
}

func postEventToCWLogs(obj interface{}, action string, conf cwLogSetting) error {
	if !*globalCWLogging {
		return nil
	}
	cwevent := []*cloudwatchlogs.InputLogEvent{}
	e := &cloudwatchlogs.InputLogEvent{}
	switch aObj := obj.(type) {
	case *v1.Event:
		msg, ts := prepareCWMessage(aObj, action)
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
