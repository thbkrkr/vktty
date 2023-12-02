### vkTTY

[vCluster](https://www.vcluster.com/docs/cli/vcluster_create) [kTTY](https://github.com/thbkrkr/ktty) pool API.

```sh
> curl http://localhost:8042/info -s | jq
{
  "capacity": 1,
  "lifetime": "2m0s",
  "parallel_creation": 4,
  "size": 5
}

> curl http://localhost:8042/ls -s | jq -c '.vclusters[]'
{"Name":"c0","ID":0,"Status":"Deleting"}
{"Name":"c1","ID":1,"Created":"2023-12-03T00:20:03.93777+01:00","Status":"Locked"}
{"Name":"c2","ID":2,"Status":"Free"}
{"Name":"c3","ID":3,"Status":"Free"}

> curl http://localhost:8042/get -s | jq
{
  "ktty": "http://z:6649-2281-18006-25764@localhost:31321"
}
```
