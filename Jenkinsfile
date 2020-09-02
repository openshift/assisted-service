String cron_string = BRANCH_NAME == "master" ? "@hourly" : ""

pipeline {
    agent { label 'centos_worker' }
    triggers { cron(cron_string) }
    environment {
        PATH = "${PATH}:/usr/local/go/bin"

        // Images
        ASSISTED_ORG = "quay.io/ocpmetal"
        ASSISTED_TAG = "${BUILD_TAG}"
        CONTAINER_BUILD_EXTRA_PARAMS = "${env.BRANCH_NAME != "master" ? "--label quay.expires-after=2d" : ""}"

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
            }
        }

        stage('Build') {
            steps {
                sh "docker login quay.io -u ${QUAY_IO_CREDS_USR} -p ${QUAY_IO_CREDS_PSW}"
                sh "make build-image build-minimal-assisted-iso-generator-image"
                sh "make update"
            }
        }

        stage('Test') {
            failFast true
            parallel {
                stage('Subsystem test') {
                    steps {
                        sh "make jenkins-deploy-for-subsystem"
                        sh "kubectl get pods -A"

                        sh '''make subsystem-run'''
                    }
                }
                stage('System test') {
                    steps {
                        // TODO: need to change to a dedicated branch
                        build job: 'assisted-test-infra/release-4.6', propagate: true, wait: true, parameters: [
                            string(name: 'SERVICE', value: "${ASSISTED_ORG}/assisted-service:${ASSISTED_TAG}")
                        ]
                    }
                }
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
        failure {
            script {
                if (env.BRANCH_NAME == 'master') {
                    script {
                        def data = [text: "Attention! ${BUILD_TAG} job failed, see: ${BUILD_URL}"]
                        writeJSON(file: 'data.txt', json: data, pretty: 4)
                    }

                    sh '''curl -X POST -H 'Content-type: application/json' --data-binary "@data.txt" https://hooks.slack.com/services/${SLACK_TOKEN}'''
                }
            }
        }

        always {
            script {
                for (service in ["assisted-service","postgres","scality","createimage"]) {
                    sh "kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep ${service} | xargs -r -I {} sh -c \"kubectl logs {} -n assisted-installer > {}.log\" || true"
                }
            archiveArtifacts artifacts: '*.log', fingerprint: true
            }
        }
    }
}
