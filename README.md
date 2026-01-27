<h1 align="center">Benchmarkoor</h1>

<p align="center">
  <img src="ui/public/img/logo_white.png" alt="Dispatchoor Logo" width="400">
</p>

## Overview

Benchmarkoor is a benchmarking tool for Ethereum execution clients. It runs standardized tests against multiple clients (Geth, Nethermind, Besu, Erigon, Reth, Nimbus) in isolated Docker containers and collects performance metrics.

## Documentation

- [Configuration Reference](docs/configuration.md) - All configuration options explained
- [Docker Guide](docs/docker.md) - Docker setup, requirements, and troubleshooting

## Docker Quickstart

The easiest way to get started is using Docker Compose:

```bash
make docker-up
```

This builds and starts:
- **benchmarkoor** - Runs benchmarks using [config.example.docker.yaml](config.example.docker.yaml)
- **ui** - Web UI available at http://localhost:8080

Results will be saved to the `./results` directory.

To view the logs you can do:
```bash
docker compose logs -f benchmarkoor
```

To stop the services:

```bash
make docker-down
```

## Development Quickstart

Build the binary
```sh
make build-core
```

Run the UI (On a different tab):
```sh
make run-ui
```

It should print the address where you can access it. By default it's http://localhost:5173/ .

Now we want to run benchmarkoor. We'll be using an example configuration file that contains some stateless tests. By default we'll be just running the `bn128` subset of that suite. Have a look at the config file for more details:
```sh
./bin/benchmarkoor run --config examples/configuration/config.stateless.yaml
```

If you don't always want to build and run, you can also use it like this:

```sh
go run cmd/benchmarkoor/*.go run --config examples/configuration/config.stateless.yaml
```

After the run, you should be able to see the results on the UI.

Note: If you want to limit your runs to a specific client that is on the config, you can either comment out those that you don't want, or use the `--limit-instance-client` flag. This allows you to limit the execution to certain clients. For example `--limit-instance-client=nethermind` would only run any instances that are of the `nethermind` client type.

Example:

```
./bin/benchmarkoor run \
      --config examples/configuration/config.stateless.yaml \
      --limit-instance-client=nethermind
```

## License

This project is licensed under the GNU General Public License v3.0 - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
