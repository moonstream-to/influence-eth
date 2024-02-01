# influence-eth

## Running this code

### From source

If you have the Go toolchain available locally, building is simple. From the root of this project, run:

```bash
go build .
```

This will create a binary called `influence-eth` in the project root. You can run it from there:

```bash
./influence-eth --help
```

Or you can move it to a location on your path. For example, if you are using Linux or a Mac:

```bash
sudo mv ./influence-eth /usr/local/bin/
```

### Prebuilt binaries

If you do not have the Go toolchain available locally, you can download the prebuilt binary appropriate to
your platform from the [latest `influence-eth` release](https://github.com/moonstream-to/influence-eth/releases/latest).

## Building a dataset of Influence.eth events

To crawl all events for an Influence.eth contract, you can use:

```bash
influence-eth events \
    --provider $STARKNET_RPC_URL \
    --contract $INFLUENCE_ETH_CONTRACT_ADDRESS \
    --batch-size 10000 \
    --from $DEPLOYMENT_BLOCK \
    --to $END_BLOCK
```

This expects the following environment variables (you can also just put them directly in the command):
1. `$STARKNET_RPC_URL`: RPC URL for a Starknet node. If you export this variable, there is no need to pass the `--provider` flag on the command line.
2. `$INFLUENCE_ETH_CONTRACT_ADDRESS`: Address of deployed Influence.eth contract.
3. `$DEPLOYMENT_BLOCK`: The block at which the contract was deployed. If you set this to 0, `influence-eth` events runs a binary search to find the deployment block automatically. If you want to find the deployment block manually, use the `influence-eth deployment-block` command.
4. `$END_BLOCK`: The block that you want to crawl until. Use `0` for a continuous crawl.

This command outputs JSON representations of the events to stdout, one event per line. To save these to a file, use a redirection:

```
influence-eth events \
    --provider $STARKNET_RPC_URL \
    --contract $INFLUENCE_ETH_CONTRACT_ADDRESS \
    --batch-size 10000 \
    --from $DEPLOYMENT_BLOCK \
    --to $END_BLOCK \
    >events.jsonl
```

This produces *raw* events. To parse these events from their representation as little more than arrays of
field elements, you can use:

```bash
influence-eth parse -i events.jsonl -o parsed-events.jsonl
```
