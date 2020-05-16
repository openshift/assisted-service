import os
import utils
import boto3
import time

def get_s3_client():
    endpoint_url = utils.get_service_url("scality")
    return boto3.client(
        's3',
        aws_access_key_id="accessKey1",
        aws_secret_access_key="verySecretKey1",
        endpoint_url=endpoint_url
    )

def main():
    retry = 20
    success = False
    while retry > 0 and success == False:
        try:
            client = get_s3_client()
            client.create_bucket(Bucket="test")
            success = True
        except Exception as e:
            print(e)
            retry -= 1
            time.sleep(5)
    if retry == 0:
        print("failed to create default s3 bucket")
        sys.exit(1)

if __name__ == "__main__":
    main()
