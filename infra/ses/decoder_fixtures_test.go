package ses

import "strings"

const fixtureBounce = `{
  "eventType":"Bounce",
  "bounce":{
    "bounceType":"Permanent",
    "bounceSubType":"General",
    "bouncedRecipients":[
      {
        "emailAddress":"recipient@example.com",
        "action":"failed",
        "status":"5.1.1",
        "diagnosticCode":"smtp; 550 5.1.1 user unknown"
      }
    ],
    "timestamp":"2017-08-05T00:41:02.669Z",
    "feedbackId":"01000157c44f053b-61b59c11-9236-11e6-8f96-7be8aexample-000000",
    "reportingMTA":"dsn; mta.example.com"
  },
  "mail":{
    "timestamp":"2017-08-05T00:40:02.012Z",
    "source":"Sender Name <sender@example.com>",
    "sourceArn":"arn:aws:ses:us-east-1:123456789012:identity/sender@example.com",
    "sendingAccountId":"123456789012",
    "messageId":"EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "destination":["recipient@example.com"],
    "headersTruncated":false,
    "headers":[
      {"name":"From","value":"Sender Name <sender@example.com>"},
      {"name":"To","value":"recipient@example.com"},
      {"name":"Subject","value":"Message sent from Amazon SES"},
      {"name":"MIME-Version","value":"1.0"},
      {"name":"Content-Type","value":"multipart/alternative; boundary=\"----=_Part_7307378_1629847660.1516840721503\""}
    ],
    "commonHeaders":{
      "from":["Sender Name <sender@example.com>"],
      "to":["recipient@example.com"],
      "messageId":"EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
      "subject":"Message sent from Amazon SES"
    },
    "tags":{
      "ses:configuration-set":["ConfigSet"],
      "ses:source-ip":["192.0.2.0"],
      "ses:from-domain":["example.com"],
      "ses:caller-identity":["ses_user"]
    }
  }
}`

const fixtureComplaint = `{
  "eventType":"Complaint",
  "complaint": {
    "complainedRecipients":[{"emailAddress":"recipient@example.com"}],
    "timestamp":"2017-08-05T00:41:02.669Z",
    "feedbackId":"01000157c44f053b-61b59c11-9236-11e6-8f96-7be8aexample-000000",
    "userAgent":"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/60.0.3112.90 Safari/537.36",
    "complaintFeedbackType":"abuse",
    "arrivalDate":"2017-08-05T00:41:02.669Z"
  },
  "mail":{
    "timestamp":"2017-08-05T00:40:01.123Z",
    "source":"Sender Name <sender@example.com>",
    "sourceArn":"arn:aws:ses:us-east-1:123456789012:identity/sender@example.com",
    "sendingAccountId":"123456789012",
    "messageId":"EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "destination":["recipient@example.com"],
    "headersTruncated":false,
    "tags":{
      "ses:configuration-set":["ConfigSet"],
      "ses:tenant-name":["ws-abc"]
    }
  }
}`

const fixtureDelivery = `{
  "eventType": "Delivery",
  "mail": {
    "timestamp": "2016-10-19T23:20:52.240Z",
    "source": "sender@example.com",
    "sourceArn": "arn:aws:ses:us-east-1:123456789012:identity/sender@example.com",
    "sendingAccountId": "123456789012",
    "messageId": "EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "destination": ["recipient@example.com"],
    "headersTruncated": false,
    "tags": {
      "ses:configuration-set": ["ConfigSet"],
      "ses:source-ip": ["192.0.2.0"],
      "ses:outgoing-ip": ["192.0.2.0"],
      "myCustomTag1": ["myCustomTagValue1"],
      "myCustomTag2": ["myCustomTagValue2"]
    }
  },
  "delivery": {
    "timestamp": "2016-10-19T23:21:04.133Z",
    "processingTimeMillis": 11893,
    "recipients": ["recipient@example.com"],
    "smtpResponse": "250 2.6.0 Message received",
    "remoteMtaIp": "123.456.789.012",
    "reportingMTA": "mta.example.com"
  }
}`

const fixtureSend = `{
  "eventType": "Send",
  "mail": {
    "timestamp": "2016-10-14T05:02:16.645Z",
    "source": "sender@example.com",
    "sendingAccountId": "123456789012",
    "messageId": "EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "destination": ["recipient@example.com"],
    "headersTruncated": false,
    "tags": {"ses:configuration-set": ["ConfigSet"]}
  },
  "send": {}
}`

const fixtureReject = `{
  "eventType": "Reject",
  "mail": {
    "timestamp": "2016-10-14T17:38:15.211Z",
    "source": "sender@example.com",
    "sendingAccountId": "123456789012",
    "messageId": "EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "destination": ["sender@example.com"],
    "headersTruncated": false,
    "tags": {"ses:configuration-set": ["ConfigSet"]}
  },
  "reject": {"reason": "Bad content"}
}`

const fixtureOpen = `{
  "eventType": "Open",
  "mail": {
    "commonHeaders": {
      "from": ["sender@example.com"],
      "messageId": "EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
      "subject": "Message sent from Amazon SES",
      "to": ["recipient@example.com"]
    },
    "destination": ["recipient@example.com"],
    "headersTruncated": false,
    "messageId": "EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "sendingAccountId": "123456789012",
    "source": "sender@example.com",
    "tags": {
      "myCustomTag1":["myCustomValue1"],
      "ses:configuration-set": ["ConfigSet"]
    },
    "timestamp": "2017-08-09T21:59:49.927Z"
  },
  "open": {
    "ipAddress": "192.0.2.1",
    "timestamp": "2017-08-09T22:00:19.652Z",
    "userAgent": "Mozilla/5.0 (iPhone; CPU iPhone OS 10_3_3 like Mac OS X) AppleWebKit/603.3.8 (KHTML, like Gecko) Mobile/14G60",
    "isBotEvent": "Unlikely"
  }
}`

const fixtureClick = `{
  "eventType": "Click",
  "click": {
    "ipAddress": "192.0.2.1",
    "link": "http://docs.aws.amazon.com/ses/latest/DeveloperGuide/send-email-smtp.html",
    "linkTags": {"samplekey0": ["samplevalue0"]},
    "timestamp": "2017-08-09T23:51:25.570Z",
    "userAgent": "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/60.0.3112.90 Safari/537.36",
    "isBotEvent": "Likely"
  },
  "mail": {
    "destination": ["recipient@example.com"],
    "headersTruncated": false,
    "messageId": "EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "sendingAccountId": "123456789012",
    "source": "sender@example.com",
    "tags": {
      "message_id": ["11111111-2222-3333-4444-555555555555"],
      "ses:configuration-set": ["ConfigSet"]
    },
    "timestamp": "2017-08-09T23:50:05.795Z"
  }
}`

const fixtureRenderingFailure = `{
  "eventType":"Rendering Failure",
  "mail":{
    "timestamp":"2018-01-22T18:43:06.197Z",
    "source":"sender@example.com",
    "sendingAccountId":"123456789012",
    "messageId":"EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "destination":["recipient@example.com"],
    "headersTruncated":false,
    "tags":{"ses:configuration-set":["ConfigSet"]}
  },
  "failure":{
    "errorMessage":"Attribute 'attributeName' is not present in the rendering data.",
    "templateName":"MyTemplate"
  }
}`

const fixtureDeliveryDelay = `{
  "eventType": "DeliveryDelay",
  "mail":{
    "timestamp":"2020-06-16T00:15:40.641Z",
    "source":"sender@example.com",
    "sendingAccountId":"123456789012",
    "messageId":"EXAMPLE7c191be45-e9aedb9a-02f9-4d12-a87d-dd0099a07f8a-000000",
    "destination":["recipient@example.com"],
    "headersTruncated":false,
    "tags":{"ses:configuration-set":["ConfigSet"]}
  },
  "deliveryDelay": {
    "timestamp": "2020-06-16T00:25:40.095Z",
    "delayType": "TransientCommunicationFailure",
    "expirationTime": "2020-06-16T00:25:40.914Z",
    "delayedRecipients": [{
      "emailAddress": "recipient@example.com",
      "status": "4.4.1",
      "diagnosticCode": "smtp; 421 4.4.1 Unable to connect to remote host"
    }]
  }
}`

const fixtureSubscription = `{
  "eventType": "Subscription",
  "mail": {
    "timestamp": "2022-01-12T01:00:14.340Z",
    "source": "sender@example.com",
    "messageId": "EXAMPLEe4bccb684-777bc8de-afa7-4970-92b0-f515137b1497-000000",
    "destination": ["recipient@example.com"]
  },
  "subscription": {
    "contactList": "ContactListName",
    "timestamp": "2022-01-12T01:00:17.910Z",
    "source": "UnsubscribeHeader"
  }
}`

const fixtureReceivedS3 = `{
  "notificationType": "Received",
  "receipt": {
    "timestamp": "2015-09-11T20:32:33.936Z",
    "processingTimeMillis": 406,
    "recipients": ["reply+token@joana.envios.example.com"],
    "spamVerdict": {"status": "PASS"},
    "virusVerdict": {"status": "PASS"},
    "spfVerdict": {"status": "PASS"},
    "dkimVerdict": {"status": "PASS"},
    "action": {
      "type": "S3",
      "topicArn": "arn:aws:sns:us-east-1:012345678912:example-topic",
      "bucketName": "my-S3-bucket",
      "objectKeyPrefix": "inbound/",
      "objectKey": "inbound/d6iitobk75ur44p8kdnnp7g2n800"
    }
  },
  "mail": {
    "timestamp": "2015-09-11T20:32:33.936Z",
    "source": "0000014fbe1c09cf-7cb9f704-7531-4e53-89a1-5fa9744f5eb6-000000@amazonses.com",
    "messageId": "d6iitobk75ur44p8kdnnp7g2n800",
    "destination": ["reply+token@joana.envios.example.com", "someone-else@example.com"],
    "headersTruncated": false,
    "commonHeaders": {
      "from": ["sender@example.com"],
      "date": "Fri, 11 Sep 2015 20:32:32 +0000",
      "to": ["reply+token@joana.envios.example.com"],
      "messageId": "<61967230-7A45-4A9D-BEC9-87CBCF2211C9@example.com>",
      "subject": "Example subject"
    }
  }
}`

const fixtureStatusDisabledCustomer = `{
  "detail-type": "Sending Status Disabled",
  "source": "aws.ses",
  "account": "012345678901",
  "time": "2025-07-24T12:44:28Z",
  "region": "us-east-1",
  "resources": ["arn:aws:ses:us-east-1:012345678901:tenant/ws-abc/tn-123"],
  "detail": {
    "version": "1.0.0",
    "data": {
      "origin": "CUSTOMER_MANAGED",
      "record": {
        "status": "DISABLED",
        "cause": "Status manually updated.",
        "lastUpdatedTimestamp": [2025, 7, 24, 12, 44, 28, 995000000]
      }
    }
  }
}`

const fixtureAdvisorOpen = `{
  "detail-type": "Advisor Recommendation Status Open",
  "source": "aws.ses",
  "account": "012345678901",
  "time": "2023-11-15T17:00:59Z",
  "region": "us-east-1",
  "resources": ["arn:aws:ses:us-east-1:012345678901:tenant/ws-abc/tn-123"],
  "detail": {
    "version": "1.0.0",
    "data": "The bounce rate exceeded 15.0% based on a representative volume of 197 emails from July 11, 2025 at 14:43 (UTC) to July 11, 2025 at 16:13 (UTC).",
    "metadata":{"impact":"HIGH","type":"BOUNCE"}
  }
}`

func fixtureStatus(origin, detailType, status string) string {
	out := strings.Replace(fixtureStatusDisabledCustomer, "CUSTOMER_MANAGED", origin, 1)
	out = strings.Replace(out, "Sending Status Disabled", detailType, 1)
	return strings.Replace(out, `"status": "DISABLED"`, `"status": "`+status+`"`, 1)
}

const fixtureReceiptSetup = `{
  "notificationType": "Received",
  "receipt": {
    "timestamp": "2026-10-22T13:00:01.120Z",
    "processingTimeMillis": 12,
    "recipients": ["recipient@example.com"],
    "spamVerdict": {"status": "PASS"},
    "virusVerdict": {"status": "PASS"},
    "spfVerdict": {"status": "PASS"},
    "dkimVerdict": {"status": "PASS"},
    "action": {
      "type": "S3",
      "topicArn": "arn:aws:sns:us-east-1:012345678912:email-inbound",
      "bucketName": "vozko-inbound",
      "objectKeyPrefix": "inbound/",
      "objectKey": "inbound/AMAZON_SES_SETUP_NOTIFICATION"
    }
  },
  "mail": {
    "timestamp": "2026-10-22T13:00:01.120Z",
    "source": "amazon-ses-setup@amazonses.com",
    "messageId": "AMAZON_SES_SETUP_NOTIFICATION",
    "destination": ["recipient@example.com"],
    "headersTruncated": false
  }
}`
