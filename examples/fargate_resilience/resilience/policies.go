package resilience

import (
	"time"

	"github.com/aponysus/recourse/budget"
	"github.com/aponysus/recourse/controlplane"
	"github.com/aponysus/recourse/observe"
	"github.com/aponysus/recourse/policy"
	"github.com/aponysus/recourse/retry"
)

func NewProvider() *controlplane.StaticProvider {
	return &controlplane.StaticProvider{
		Default: policy.New("default.unmapped",
			policy.PolicyID("UNMAPPED_safe_default_v1"),
			policy.MaxAttempts(1),
		),
		Policies: map[policy.PolicyKey]policy.EffectivePolicy{
			key(KeySecretsManagerGetSecretValue): awsRead(KeySecretsManagerGetSecretValue, "aws-secrets-get-v1"),

			key(KeyS3GetObjectConfig):   awsRead(KeyS3GetObjectConfig, "aws-s3-config-get-v1"),
			key(KeyS3GetObjectPKConfig): awsRead(KeyS3GetObjectPKConfig, "aws-s3-pkconfig-get-v1"),
			key(KeyS3GetObjectChecksum): awsRead(KeyS3GetObjectChecksum, "aws-s3-checksum-get-v1"),
			key(KeyS3PutObjectChecksum): awsWriteSmall(KeyS3PutObjectChecksum, "aws-s3-checksum-put-v1"),

			key(KeyS3UploadData): s3UploadData(KeyS3UploadData, "aws-s3-upload-data-v1"),
			key(KeyS3PutFormat):  awsWriteSmall(KeyS3PutFormat, "aws-s3-format-put-v1"),

			key(KeyStepFunctionsSendTaskSuccess): stepFnCallback(KeyStepFunctionsSendTaskSuccess, "aws-sfn-success-v1"),
			key(KeyStepFunctionsSendTaskFailure): stepFnCallback(KeyStepFunctionsSendTaskFailure, "aws-sfn-failure-v1"),

			key(KeySQLServerPing):          sqlPing(KeySQLServerPing, "sql-ping-v1"),
			key(KeySQLServerQueryChecksum): sqlQuery(KeySQLServerQueryChecksum, "sql-query-checksum-v1"),
			key(KeySQLServerQueryMetadata): sqlQuery(KeySQLServerQueryMetadata, "sql-query-metadata-v1"),

			key(KeyEventingEmit): eventEmit(KeyEventingEmit, "eventing-emit-v1"),
		},
	}
}

func NewBudgets() *budget.Registry {
	r := budget.NewRegistry()
	r.MustRegister("aws_read_retry", budget.NewTokenBucketBudget(12, 3))
	r.MustRegister("aws_write_retry", budget.NewTokenBucketBudget(4, 1))
	r.MustRegister("db_retry", budget.NewTokenBucketBudget(8, 2))
	r.MustRegister("event_retry", budget.NewTokenBucketBudget(20, 5))
	return r
}

func NewExecutor(obs observe.Observer) *retry.Executor {
	if obs == nil {
		obs = &observe.NoopObserver{}
	}

	return retry.NewExecutor(
		retry.WithProvider(NewProvider()),
		retry.WithBudgetRegistry(NewBudgets()),
		retry.WithObserver(obs),

		retry.WithClassifier(ClassifierAWSRead, AWSReadClassifier{}),
		retry.WithClassifier(ClassifierAWSWriteSafe, AWSWriteSafeClassifier{}),
		retry.WithClassifier(ClassifierAWSCallback, AWSCallbackClassifier{}),
		retry.WithClassifier(ClassifierSQLConnect, SQLConnectClassifier{}),
		retry.WithClassifier(ClassifierSQLQuery, SQLQueryClassifier{}),
		retry.WithClassifier(ClassifierEventing, EventingClassifier{}),

		retry.WithMissingPolicyMode(retry.FailureDeny),
		retry.WithMissingClassifierMode(retry.FailureDeny),
		retry.WithMissingBudgetMode(retry.FailureDeny),
		retry.WithRecoverPanics(true),
	)
}

func awsRead(key, id string) policy.EffectivePolicy {
	return policy.New(key,
		policy.PolicyID(id),
		policy.MaxAttempts(2),
		policy.ExponentialBackoff(100*time.Millisecond, 2*time.Second),
		policy.PerAttemptTimeout(3*time.Second),
		policy.OverallTimeout(8*time.Second),
		policy.Classifier(ClassifierAWSRead),
		policy.Budget("aws_read_retry"),
	)
}

func awsWriteSmall(key, id string) policy.EffectivePolicy {
	return policy.New(key,
		policy.PolicyID(id),
		policy.MaxAttempts(2),
		policy.ExponentialBackoff(200*time.Millisecond, 3*time.Second),
		policy.PerAttemptTimeout(5*time.Second),
		policy.OverallTimeout(12*time.Second),
		policy.Classifier(ClassifierAWSWriteSafe),
		policy.Budget("aws_write_retry"),
	)
}

func s3UploadData(key, id string) policy.EffectivePolicy {
	return policy.New(key,
		policy.PolicyID(id),
		policy.MaxAttempts(1),
		policy.Classifier(ClassifierAWSWriteSafe),
	)
}

func stepFnCallback(key, id string) policy.EffectivePolicy {
	return policy.New(key,
		policy.PolicyID(id),
		policy.MaxAttempts(1),
		policy.Classifier(ClassifierAWSCallback),
	)
}

func sqlPing(key, id string) policy.EffectivePolicy {
	return policy.New(key,
		policy.PolicyID(id),
		policy.MaxAttempts(3),
		policy.ExponentialBackoff(250*time.Millisecond, 5*time.Second),
		policy.PerAttemptTimeout(5*time.Second),
		policy.OverallTimeout(20*time.Second),
		policy.Classifier(ClassifierSQLConnect),
		policy.BudgetWithCost("db_retry", 2),
	)
}

func sqlQuery(key, id string) policy.EffectivePolicy {
	return policy.New(key,
		policy.PolicyID(id),
		policy.MaxAttempts(2),
		policy.ExponentialBackoff(250*time.Millisecond, 3*time.Second),
		policy.PerAttemptTimeout(10*time.Second),
		policy.OverallTimeout(25*time.Second),
		policy.Classifier(ClassifierSQLQuery),
		policy.Budget("db_retry"),
	)
}

func eventEmit(key, id string) policy.EffectivePolicy {
	return policy.New(key,
		policy.PolicyID(id),
		policy.MaxAttempts(2),
		policy.ExponentialBackoff(100*time.Millisecond, time.Second),
		policy.PerAttemptTimeout(3*time.Second),
		policy.OverallTimeout(5*time.Second),
		policy.Classifier(ClassifierEventing),
		policy.Budget("event_retry"),
	)
}
