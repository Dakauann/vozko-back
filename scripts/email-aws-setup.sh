#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 <hml|prd> [--with-iam-user]" >&2
  exit 1
}

[ $# -ge 1 ] || usage
ENVIRONMENT=$1
WITH_IAM=${2:-}
case "$ENVIRONMENT" in hml|prd) ;; *) usage ;; esac
[ -z "$WITH_IAM" ] || [ "$WITH_IAM" = "--with-iam-user" ] || usage

R=${EMAIL_AWS_REGION:-sa-east-1}
A=$(aws sts get-caller-identity --query Account --output text)
P=vozko-email-$ENVIRONMENT
B=$P-inbound-$A

topic_policy() {
  printf '{"Version":"2012-10-17","Statement":[{"Sid":"Owner","Effect":"Allow","Principal":{"AWS":"arn:aws:iam::%s:root"},"Action":["SNS:GetTopicAttributes","SNS:SetTopicAttributes","SNS:AddPermission","SNS:RemovePermission","SNS:DeleteTopic","SNS:Subscribe","SNS:ListSubscriptionsByTopic","SNS:Publish"],"Resource":"%s"},{"Sid":"SES","Effect":"Allow","Principal":{"Service":"ses.amazonaws.com"},"Action":"sns:Publish","Resource":"%s","Condition":{"StringEquals":{"AWS:SourceAccount":"%s"}}},{"Sid":"EventBridge","Effect":"Allow","Principal":{"Service":"events.amazonaws.com"},"Action":"sns:Publish","Resource":"%s","Condition":{"ArnLike":{"aws:SourceArn":"arn:aws:events:%s:%s:rule/%s-*"}}}]}' "$A" "$1" "$1" "$A" "$1" "$R" "$A" "$P"
}

EV=$(aws sns create-topic --region "$R" --name "$P-events" --query TopicArn --output text)
IN=$(aws sns create-topic --region "$R" --name "$P-inbound" --query TopicArn --output text)
aws sns set-topic-attributes --region "$R" --topic-arn "$EV" --attribute-name Policy --attribute-value "$(topic_policy "$EV")"
aws sns set-topic-attributes --region "$R" --topic-arn "$IN" --attribute-name Policy --attribute-value "$(topic_policy "$IN")"

aws s3api head-bucket --bucket "$B" 2>/dev/null || aws s3api create-bucket --bucket "$B" --region "$R" --create-bucket-configuration LocationConstraint="$R" >/dev/null
aws s3api put-public-access-block --bucket "$B" --public-access-block-configuration BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true
aws s3api put-bucket-policy --bucket "$B" --policy "$(printf '{"Version":"2012-10-17","Statement":[{"Sid":"SESInbound","Effect":"Allow","Principal":{"Service":"ses.amazonaws.com"},"Action":"s3:PutObject","Resource":"arn:aws:s3:::%s/inbound/*","Condition":{"StringEquals":{"AWS:SourceAccount":"%s"}}}]}' "$B" "$A")"
aws s3api put-bucket-lifecycle-configuration --bucket "$B" --lifecycle-configuration '{"Rules":[{"ID":"expire-inbound","Status":"Enabled","Filter":{"Prefix":"inbound/"},"Expiration":{"Days":30}}]}' >/dev/null

aws ses describe-receipt-rule-set --region "$R" --rule-set-name "$P" >/dev/null 2>&1 || aws ses create-receipt-rule-set --region "$R" --rule-set-name "$P"
aws ses set-active-receipt-rule-set --region "$R" --rule-set-name "$P"

aws events put-rule --region "$R" --name "$P-tenant-status" --event-pattern '{"source":["aws.ses"],"detail-type":["Sending Status Enabled","Sending Status Disabled","Advisor Recommendation Status Open","Advisor Recommendation Status Resolved"]}' >/dev/null
aws events put-targets --region "$R" --rule "$P-tenant-status" --targets "Id=events-topic,Arn=$EV" >/dev/null

if [ "$WITH_IAM" = "--with-iam-user" ]; then
  POLICY_ARN=arn:aws:iam::$A:policy/$P-backend
  POLICY_DOC=$(printf '{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":"ses:*","Resource":"*","Condition":{"StringEquals":{"aws:RequestedRegion":"%s"}}},{"Effect":"Allow","Action":["s3:GetObject","s3:DeleteObject"],"Resource":"arn:aws:s3:::%s/inbound/*"}]}' "$R" "$B")
  if aws iam get-policy --policy-arn "$POLICY_ARN" >/dev/null 2>&1; then
    for version in $(aws iam list-policy-versions --policy-arn "$POLICY_ARN" --query 'Versions[?IsDefaultVersion==`false`].VersionId' --output text); do
      aws iam delete-policy-version --policy-arn "$POLICY_ARN" --version-id "$version"
    done
    aws iam create-policy-version --policy-arn "$POLICY_ARN" --policy-document "$POLICY_DOC" --set-as-default >/dev/null
  else
    aws iam create-policy --policy-name "$P-backend" --policy-document "$POLICY_DOC" >/dev/null
  fi
  aws iam get-user --user-name "$P-backend" >/dev/null 2>&1 || aws iam create-user --user-name "$P-backend" >/dev/null
  aws iam attach-user-policy --user-name "$P-backend" --policy-arn "$POLICY_ARN"
  echo "IAM user $P-backend is ready; create its key with: aws iam create-access-key --user-name $P-backend"
fi

echo "EMAIL_AWS_REGION=$R"
echo "EMAIL_AWS_ACCOUNT_ID=$A"
echo "EMAIL_SNS_EVENTS_TOPIC_ARN=$EV"
echo "EMAIL_SNS_INBOUND_TOPIC_ARN=$IN"
echo "EMAIL_SNS_ALLOWED_TOPIC_ARNS=$EV,$IN"
echo "EMAIL_INBOUND_BUCKET=$B"
echo "ACTIVE_RECEIPT_RULE_SET=$(aws ses describe-active-receipt-rule-set --region "$R" --query Metadata.Name --output text)"
