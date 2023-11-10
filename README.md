# podwiz

## podwiz_client

Download the binary from the release and run it. (Support on systemctl soon)

## podwiz tool

### Create a shell

`./podwiz -n level1 -m jade -p ./chall-1 -i game -sn shell-1 -t 120 start`

**`-n`** for the username (must be same as mentioned in Dockerfile).

**`-m`** machine name.

**`-p`** path of the directory containing Dockerfile and pod.yaml (The names must be same as mentioned).

**`-i`** name of the docker image (built if not available) to be used (make sure that the shell you ran podwiz_client into is pointing to the k8s' docker daemon).

**`-sn`** name of the schedule.

**`-t`** time (secs) after which you want to delete shell.

**Output:**
```
Username: level1
Password: xBQ5wboh8pt7Rbm
Port: 45165
```

### List schedules

`./podwiz list`

**Output:**
```
+---------+------------+-------------------------------+-------------------------------+
|  NAME   |  POD NAME  |             START             |            FINISH             |
+---------+------------+-------------------------------+-------------------------------+
| shell-1 | jade5g0f2f | 2023-11-11 01:55:25 +0530 IST | 2023-11-11 01:57:25 +0530 IST |
+---------+------------+-------------------------------+-------------------------------+
```
