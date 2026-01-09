## Pushing and pulling agents from Docker Hub

### `cagent push`

Agent configurations can be packaged and shared to Docker Hub using the `cagent
push` command

```sh
cagent push ./<agent-file>.yaml namespace/reponame
```

Docker `cagent` will automatically build an OCI image and push it to the desired
repository using your Docker credentials

### `cagent pull`

Pulling agents from Docker Hub is also just one `cagent pull` command away.

```sh
cagent pull agentcatalog/pirate
```

`cagent` will pull the image, extract the .yaml file and place it in your
working directory for ease of use.

`cagent run agentcatalog_pirate.yaml` will run your newly pulled agent
