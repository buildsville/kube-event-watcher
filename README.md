kube-event-watcher
=====

This tool is used to notify kubernetes events to Slack and AWS CloudwatchLogs.  
In short, it's like Slack and CWLogs version below.  
https://kubernetes.io/docs/tasks/debug-application-cluster/events-stackdriver/  
https://github.com/GoogleCloudPlatform/k8s-stackdriver/tree/master/event-exporter/  

## How to use
```
$ go build -o kube-event-watcher src/*.go
$ ./kube-event-watcher
```

## Settings

### Environment variable
Slack api token and default notification channel.  
(Notification channel can be further configured with config)  

```
SLACK_TOKEN=xoxb-1234567890-abcdefghijk
SLACK_CHANNEL=k8s-events
```

Path of kubeconfig (optional)  
Generally use ServiceAccount in manifest, so don't need this.  

```
KUBECONFIG=/path/to/kubeconfig/file
```

### Flags

```
-config string
    Path to config file. (default "~/.kube-event-watcher/config.yaml")
-notifySlack bool
    Whether to notify events to Slack. (default "true")
-cwLogging bool
    Whether to logging events to Cloudwatch logs. (default "false")
-cwLogGroup string
    Loggroup name on logging. (default "kube-event-watcher")
-cwLogStream string
    Logstream name on logging. (default "event")
-listen-address string
    The address to promtheus metrics endpoint. (default ":9297")
-kubeconfig string
    Path to kubeconfig file. Generally use ServiceAccount in manifest, so don't need this. (default "~/.kube/config")
-logtostderr bool
    log to standard error instead of files. (default "false")
```

Can reference all flags with `/kube-event-watcher -h`

### Config file
Configure events to be notified in yaml format file.  

```
- namespace: "namespace"
  watchEvent:
    ADDED: true
    MODIFIED: true
    DELETED: false
  fieldSelectors:
    - key: key1
      value: value1
      type: exclude
    - key: key2
      value: value2
      type: include
  channel: overwrite-notify-channel
  logStream: overwrite-CWLogs-stream
```

- `namespace` : the namespace to be notified. For all namespaces, specify `""`.
- `watchevent` : Set `true` if want to notify, `false` if don't need it.
  - `ADDED` : Newly created events.
  - `MODIFIED` : Existing event happens again etc.
  - `DELETED` : Delete events due to expiration etc. Generally set `false`.
- `fieldSelectors` : Can specify details of events you want to notify. __It's AND condition.__
  - If this section is not set, all events will be notified.
  - Refer to the official document for fields that can be specified.
  - If `type: include` is set, it is set `equal`, and in case of `type: exclude` it is set with `not equal`.
    - `type: exclude` is effective when you want to exclude a part of a wide range.
    - This section is not set or invalid value, `type: include` is set by default.
  - Please also refer to `examples/config.yaml`.
- `channel` : Set when you want to change the channel to be notified.
  - Channel is not found, events will be sent to default channel.
- `logStream` : Set when you want to change the log stream to be put.
  - Stream is not found, events will be sent to default stream.

## Notification example

<img src="https://i.imgur.com/aZ7CbfT.jpg">

Green if the type of event is `Normal`, and yellow in the case of `Warning`.  

## docker container

https://hub.docker.com/r/masahata/kube-event-watcher/

## in kubernetes

Required permissions below.

```
apiGroups: [""]
resources: ["events"]
verbs: ["get", "watch", "list"]
```

See also `examples/deploy.yaml`.

## prometheus metrics
By default, prometheus metrics is in `address=:9297` `path=/metrics`.  
Output metrics only `ew_event_count`, it's a counter metric with the value of each field as label.  
Listen address can be changed with flag.  

## Clowdwatch Logs
Can also send events to Cloudwatch Logs.  
Required IAM policy is below.  

```
logs:CreateLogGroup
logs:CreateLogStream
logs:PutLogEvents
logs:DescribeLogStreams
logs:DescribeLogGroups
```

For setting, see Flags and Config sections.  
