#!/usr/bin/env bats

@test "adds numbers" {
  [ "$((1 + 1))" -eq 2 ]
}
