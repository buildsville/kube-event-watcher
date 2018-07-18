kube-event-watcher
=====

kubernetes内で起こったeventをslackに通知するツールです  
やっていることは <a href="https://github.com/bitnami-labs/kubewatch">kubewatch</a> と似ていますが、あちらはpodなどのresourceをwatchしているのに対し、こちらはeventをwatchしています  


## 使い方
```
$ go build
$ ./kube-event-watcher
```

## 設定

### 環境変数
slackのAPI Tokenと通知先のchannelを環境変数に設定します

```
SLACK_TOKEN=xoxb-hogehagehigehege
SLACK_CHANNEL=hogeroom
```

必要な場合はkubeconfigのpathも環境変数で指定します

```
KUBECONFIG=/path/to/kubeconfig/file
```

必要かつ指定がない場合は`$HOME/.kube/config`を参照します

kubernetesにdeployする場合は不要みたいです（apiのInClusterConfig()でよろしくやってくれるようです）


### 設定ファイル
yaml形式のconfigファイルで通知するイベントを設定します

例）
```
- namespace: "namespace"
  watchEvent:
    ADDED: true
    MODIFIED: true
    DELETED: false
  fieldSelectors:
    - key: key1
      value: value1
      except: true
    - key: key2
      value: value2
      except: false
  channel: config-ch
```

- `namespace` : 通知対象のnamespaceです、`""`で全て対象になります
- `watchevent` : 通知したい時はtrue、不要な時はfalseを設定します
  - `ADDED` : 新しいイベントです。大体はこれかと思います
  - `MODIFIED` : 既存のイベントが再度起こった時などに使われます
  - `DELETED` : イベントの保持期間が切れて削除された時などに起こります、基本的に不要かと思います
- `fieldSelectors` : 通知したいイベントの詳細を指定できます。__複数指定はAND条件になります__
  - 指定がない場合は全てのeventを取得します
  - 指定できるfield keyは <a href="https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.11/#event-v1-core">apiリファレンス</a> を参照してください
  - `except:true`を設定した場合、そのfieldSelectorはnot equalで設定されます
  - `examples/config.yaml`も併せて参考にしてください
- `channel` : 環境変数に設定したslackのチャンネルと送信先を分けたい時に設定します
  - notFoundになった場合は環境変数で設定したチャンネルに送信されます

起動時にコマンドライン引数 `-config` でpathを指定できます

```
$ ./kube-event-watcher -config=/PATH/TO/CONFIG/config.yaml
```

指定がない場合は `~/.kube-event-watcher/config.yaml` を参照します

## 通知例
大体こんな感じです

<img src="https://i.imgur.com/aZ7CbfT.jpg">


eventのtypeが`Normal`の場合は緑、`Warning`の場合は黄色で通知されます  
この2種類しか確認していないのでその他のtypeが出た場合は赤で通知するようにしています

## dockerコンテナ

https://hub.docker.com/r/masahata/kube-event-watcher/

## kubernetesデプロイ

eventをwatchするパーミッションが必要です  
`examples/kubernetes.yaml`を参考にしてください

## prometheusメトリクス
`address=:9297` `path=/metrics` にprometheusのmetricsを出しています  
出力されるメトリクスは `ew_event_count` のみで、各fieldの内容をlabelに持ったカウンターです  
addressはコマンドライン引数 `-listen-address` で変更できます  
