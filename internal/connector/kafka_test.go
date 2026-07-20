package connector

import "testing"

func TestKafkaBrokerValidationRejectsPrivateAndHonorsExplicitAllowlist(t *testing.T) {
	if err := validateKafkaBroker("127.0.0.1:9092", nil); err == nil {
		t.Fatal("private Kafka broker must be rejected by default")
	}
	if err := validateKafkaBroker("127.0.0.1:9092", []string{"127.0.0.1:9092"}); err != nil {
		t.Fatalf("allowlisted Kafka broker rejected: %v", err)
	}
}

func TestKafkaBrokerValidationRequiresHostPort(t *testing.T) {
	if err := validateKafkaBroker("kafka.example", nil); err == nil {
		t.Fatal("broker without port must be rejected")
	}
}
