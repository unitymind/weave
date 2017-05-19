# Entrypoints

This document describes the various ways that weave as a binary gets launched, and where and how it processes options.

The goal is to enable maintainers to modify functionality without having to reverse engineer the code to find all of the places you can start a weave binary and what it depends upon.

## Components
### weaveutil
weaveutil is the primary binary for managing a existing weave network. It gets information about the network, attaches and detaches containers, etc.

For the majority of interactions with an existing weave network, you will launch `weaveutil` in some manner or another.

Almost all options can be passed as `--option` to `weaveutil`. These options are created in `prog/weaveutil/main.go` with a table mapping each command to a dedicated golang function.

In addition to operating in normal mode, `weaveutil` has two additional operating modes. Before processing any commands, `weaveutil` checks the filename with which it was called.

* If it was `weave-ipam` it delegates responsibility to the `cniIPAM()` function in `prog/weaveutil/cni.go`, which, in turn, calls the standard CNI plugin function `cni.PluginMain()`, passing it weave's IPAM implementation from `plugin/ipam`.
* If it was `weave-net` it delegates responsibility to the `cniNet()` function in `prog/weaveutil/cni.go`, which, in turn, calls the standard CNI plugin function `cni.PluginMain()`, passing it weave's net plugin implementation from `plugin/net`.

### weave
Wrapping `weaveutil` is `weave`, a `sh` script that provides help information and calls `weaveutil` as relevant.

### weave-kube
weave-kube is an image with the weave binaries and a wrapper script installed. It is responsible for:

1. Setting up the weave network
2. Connecting to peers
3. Copy the weave CNI-compatible binary plugin `weaveutil` to `/opt/cni/bin/weave-net` and weave config in `/etc/cni/net.d/10-weave.conf`
4. Running the weave network policy controller (weave-npc) to implement kubernetes' `NetworkPolicy`

Once installation is complete, each network-relevant change in a container leads `kubelet` to:

1. Set certain environment variables to configure the CNI plugin, mostly related to the container ID
2. Launch `/opt/cni/bin/weave-net`
3. Pass the CNI config - the contents of `/etc/cni/net.d/10-weave.conf` to `weave-net`

Thus, when each container is changed, and `kubelet` calls weave as a CNI plugin, it really just is launching `weaveutil` as `weave-net`.

The installation and setup of all of the above - and therefore the entrypoint to the weave-kube image - is the script `prog/weave-kube/launch.sh`. `launch.sh` does the following:

1. Read configuration variables from the environment. When the documentation for `weave-kube` describes configuring the weave network by changing the environment variables in the daemonset in the `.yml` file, `launch.sh` reads these environment variables.
2. Set up the config file at `/etc/cni/net.d/10-weave.conf`
3. Run the `weave` initialization script
4. Run `weaver` with the correct configuration passed as command-line options


## Adding Options
To add new options to how weave should run with each invocation, you would do the following:

1. Add a command-line option `--option` to `weaveutil`
2. Add the option to `weave` script help. You do not need to add it to how it calls `weaveutil` since `weave` just passes all options on.
3. If it can be configured for CNI:
    * have `prog/weave-kube/launch.sh` read it as an environment variable and set inside `/etc/cni/net.d/10-weave.conf`
    * set a default for the environment variable in `prog/weave-kube/weave-daemonset-k8s-1.6.yaml` and `weave-daemonset.yaml`
    * have the CNI code read it from the config in `plugin/net/cni.go` and use where appropriate
4. Document it!
