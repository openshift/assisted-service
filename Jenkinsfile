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
                sh 'make clear-all || true'
                sh 'make ci-lint'
            }
        }

        stage('Build') {
            steps {
                sh "make build-image build-minimal-assisted-iso-generator-image"
                sh "make jenkins-deploy-for-subsystem"
                sh "kubectl get pods -A"
            }
        }

        stage('Subsystem Test') {
            steps {
                sh "make subsystem-run"
            }
        }

        stage('Publish') {
            when { branch 'master'}
            steps {
                sh "docker login quay.io -u ${QUAY_IO_CREDS_USR} -p ${QUAY_IO_CREDS_PSW}"
                sh "make publish"
            }
        }
    }

    post {
        always {
            script {
                if ((env.BRANCH_NAME == 'master') && (currentBuild.currentResult == "ABORTED" || currentBuild.currentResult == "FAILURE")){
                    script {
                        def data = [text: "Attention! ${BUILD_TAG} job failed, see: ${BUILD_URL}"]
                        writeJSON(file: 'data.txt', json: data, pretty: 4)
                    }

                    sh '''curl -X POST -H 'Content-type: application/json' --data-binary "@data.txt" https://hooks.slack.com/services/${SLACK_TOKEN}'''
                }

                for (service in ["assisted-service","postgres","scality","createimage"]) {
                    sh "kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep ${service} | xargs -r -I {} sh -c \"kubectl logs {} -n assisted-installer > {}.log\" || true"
                }
            archiveArtifacts artifacts: '*.log', fingerprint: true
            }
        }
    }
}
