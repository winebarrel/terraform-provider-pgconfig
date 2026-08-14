package pgclient

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// GetRDSAuthToken builds an RDS IAM auth token to use as the connection
// password. It follows the same profile/region/assume-role precedence as
// cyrilgdn/terraform-provider-postgresql's aws_rds_iam_auth support.
func GetRDSAuthToken(ctx context.Context, region, profile, roleARN, username, host string, port int64) (string, error) {
	endpoint := fmt.Sprintf("%s:%d", host, port)

	var awscfg aws.Config
	var err error

	switch {
	case profile != "":
		awscfg, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithSharedConfigProfile(profile))
	case region != "":
		awscfg, err = awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	default:
		awscfg, err = awsconfig.LoadDefaultConfig(ctx)
	}

	if err != nil {
		return "", fmt.Errorf("could not load AWS default config: %w", err)
	}

	if roleARN != "" {
		stsClient := sts.NewFromConfig(awscfg)
		roleOutput, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
			RoleArn:         aws.String(roleARN),
			RoleSessionName: aws.String("TerraformPgconfigProvider"),
		})

		if err != nil {
			return "", fmt.Errorf("could not assume AWS role: %w", err)
		}

		awscfg, err = awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithCredentialsProvider(
				aws.NewCredentialsCache(credentials.NewStaticCredentialsProvider(
					*roleOutput.Credentials.AccessKeyId,
					*roleOutput.Credentials.SecretAccessKey,
					*roleOutput.Credentials.SessionToken,
				)),
			),
		)

		if err != nil {
			return "", fmt.Errorf("could not load AWS default config: %w", err)
		}
	}

	token, err := auth.BuildAuthToken(ctx, endpoint, awscfg.Region, username, awscfg.Credentials)

	if err != nil {
		return "", fmt.Errorf("could not build RDS auth token: %w", err)
	}

	return token, nil
}
