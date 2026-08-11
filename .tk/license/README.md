# License Taskfile

This repository contains a reusable Taskfile that provides license file generation for projects. The
tasks included in this `Taskfile` use the [license](https://github.com/nishanths/license) tool.

<!-- TOC -->
* [License Taskfile](#license-taskfile)
  * [Summary](#summary)
  * [Prerequisites](#prerequisites)
  * [Configuration](#configuration)
  * [Available Tasks](#available-tasks)
  * [Usage](#usage)
<!-- TOC -->

## Summary

This `Taskfile` provides a set of tasks that help you generate and manage license files for your project. It
automatically fetches project details (author name, project name, year) from your environment using `gh` (GitHub CLI)
and generates a standardized license file.

## Prerequisites

* [Task](https://taskfile.dev/): A task runner / simpler Make alternative written in Go.
* [license](https://github.com/nishanths/license): A command-line tool for generating license files.
* [gh](https://cli.github.com/): GitHub CLI, used to fetch author and project information.

Install these tools before proceeding.

## Configuration

The `Taskfile` comes with these configurable variables:

* `DEFAULT_LICENSE_PACKAGE`: The Go package for the license tool. Default: `github.com/nishanths/license/v5`
* `DEFAULT_LICENSE_BIN_NAME`: The binary name of the license tool. Default: `license`
* `DEFAULT_LICENSE_VERSION`: The version to install. Default: `latest`
* `DEFAULT_LICENSE_TYPE`: The type of license to generate. Default: `mit`
* `DEFAULT_LICENSE_FILENAME`: The output filename. Default: `LICENSE.md`
* `DEFAULT_LICENSE_NAME`: The license holder name, fetched from GitHub CLI.
* `DEFAULT_LICENSE_YEAR`: The license year, defaults to the current year.
* `DEFAULT_LICENSE_PROJECT`: The project name, fetched from GitHub CLI.

You can override these variables by redefining them in your environment or in your `Taskfile`.

## Available Tasks

Here are the tasks that the `Taskfile` provides:

* `generate`: Generates a license file using project details. Skips generation if the license file already exists.
* `run`: Runs the license tool directly with custom arguments.
* `install`: Installs the license tool as a Go tool dependency.
* `uninstall`: Removes the license tool from Go tool dependencies.

## Usage

To use this `Taskfile`, include it in your project's `Taskfile`. For example:

```yaml
version: '3'

includes:
  license: .tk/license/Taskfile.yaml
```

Generate a license file:

```shell
task license:generate
```

Customize the license holder name:

```shell
LICENSE_NAME='Jane Doe <jane.doe@example.com>' task license:generate
```
