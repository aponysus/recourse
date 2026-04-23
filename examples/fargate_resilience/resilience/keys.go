package resilience

import "github.com/aponysus/recourse/policy"

const (
	KeySecretsManagerGetSecretValue = "aws.secretsmanager.get_secret_value"

	KeyS3GetObjectConfig   = "aws.s3.getobject.config"
	KeyS3GetObjectPKConfig = "aws.s3.getobject.pkconfig"
	KeyS3GetObjectChecksum = "aws.s3.getobject.checksum"
	KeyS3PutObjectChecksum = "aws.s3.putobject.checksum"

	KeyS3UploadData = "aws.s3.upload.data"
	KeyS3PutFormat  = "aws.s3.putobject.format"

	KeyStepFunctionsSendTaskSuccess = "aws.sfn.send_task_success"
	KeyStepFunctionsSendTaskFailure = "aws.sfn.send_task_failure"

	KeySQLServerPing          = "db.sqlserver.ping"
	KeySQLServerQueryChecksum = "db.sqlserver.query.checksum"
	KeySQLServerQueryMetadata = "db.sqlserver.query.metadata"

	KeyEventingEmit = "eventing.emit"
)

const (
	ClassifierAWSRead      = "aws_read"
	ClassifierAWSWriteSafe = "aws_write_safe"
	ClassifierAWSCallback  = "aws_callback"
	ClassifierSQLConnect   = "sql_connect"
	ClassifierSQLQuery     = "sql_query"
	ClassifierEventing     = "eventing"
)

func key(s string) policy.PolicyKey {
	return policy.ParseKey(s)
}
