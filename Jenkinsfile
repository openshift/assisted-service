String cron_string = BRANCH_NAME == "master" ? "@hourly" : ""

pipeline {
    agent { label 'centos_worker' }
    triggers { cron(cron_string) }
    environment {
        PATH = "${PATH}:/usr/local/go/bin"

        // Images
        SERVICE = "quay.io/ocpmetal/assisted-service:${BUILD_TAG}"
        ISO_CREATION = "quay.io/ocpmetal/assisted-iso-create:${BUILD_TAG}"

        // Credentials
        SLACK_TOKEN = credentials('slack-token')
        QUAY_IO_CREDS = credentials('ocpmetal_cred')
    }
    options {
      timeout(time: 1, unit: 'HOURS') 
    }

    stages {
        stage('Init') {
            steps {
                sh 'echo $CHANGE_ID'
            }
        }
    }
}
