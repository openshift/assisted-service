#!/usr/bin/python

import boto3
import os
import time

S3_ENDPOINT_URL = os.environ.get('S3_ENDPOINT_URL')
S3_BUCKET = os.environ.get('S3_BUCKET', 'test')
AWS_ACCESS_KEY_ID = os.environ.get('AWS_ACCESS_KEY_ID', 'accessKey1')
AWS_SECRET_ACCESS_KEY = os.environ.get('AWS_SECRET_ACCESS_KEY', 'verySecretKey1')
S3_OBJECT_RETENTION_MINUTES = int(os.environ.get('S3_OBJECT_RETENTION_MINUTES', '60'))
ISO_PREFIX = os.environ.get('ISO_PREFIX', 'discovery-image')

def should_expire(obj_name, obj_tags):
    for obj_tag in obj_tags.get('TagSet', []):
        if obj_tag['Key'] == 'create_sec_since_epoch':
            create_time = int(obj_tag['Value'])
            if create_time + (60 * S3_OBJECT_RETENTION_MINUTES) < time.time():
                return True
    return False

def handle_object(client, obj):
    obj_tags = client.get_object_tagging(Bucket=S3_BUCKET, Key=obj['Key'])
    if should_expire(obj['Key'], obj_tags):
        print('Deleting expired: ' + obj['Key'])
        client.delete_object(Bucket=S3_BUCKET, Key=obj['Key'])

def main():
    client = boto3.client('s3', use_ssl=False, endpoint_url=S3_ENDPOINT_URL,
                          aws_access_key_id=AWS_ACCESS_KEY_ID,
                          aws_secret_access_key=AWS_SECRET_ACCESS_KEY)
    paginator = client.get_paginator('list_objects')
    operation_parameters = {'Bucket': S3_BUCKET, 'Prefix': ISO_PREFIX}
    page_iterator = paginator.paginate(**operation_parameters)
    for page in page_iterator:
        if not page.get('Contents'):
            continue
        for obj in page['Contents']:
            handle_object(client, obj)

if __name__ == "__main__":
    main()
