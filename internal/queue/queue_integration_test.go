//go:build integration

package queue

// Integration tests for RedisQueue require testcontainers-go and a Docker daemon.
// TODO: implement TestRedisQueue_Integration using testcontainers-go when wiring CI.
// The test should: spin up a Redis container, Push a StreamTweet to QueueRaw,
// Pop it back, and verify the full round-trip.
