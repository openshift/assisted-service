String cron_string = BRANCH_NAME == "master" ? "@hourly" : ""

pipeline {
    agent { label 'centos_worker' }
    triggers { cron(cron_string) }
    environment {
        PATH = "${PATH}:/usr/local/go/bin"

        // Images
        ASSISTED_ORG = "quay.io/ocpmetal"
        ASSISTED_TAG = "${BUILD_TAG}"

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
                sh "make update"
            }
        }

        stage('Test') {
            failFast true
            parallel {
                stage('K8s') {
                    steps {
                        sh "make jenkins-deploy-for-subsystem"
                        sh "kubectl get pods -A"

                        sh "make subsystem-run"
                    }
                }
                stage("OnPrem") {
                    agent { label 'centos_worker' }
                    stages {
                        stage('OnPrem - Init') {
                            steps {
                                sh 'make clean-onprem || true'
                            }
                        }

                        stage("OnPrem - Build") {
                            steps {
                                sh "make build-onprem"
                                sh "make deploy-onprem"
                            }
                        }
                        stage("OnPrem - Test") {
                            steps {

                                sh "make test-onprem"
                            }
                        }
                    }

                    post {
                        always {
                            script {
                                sh "make clean-onprem"
                            }
                        }
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
