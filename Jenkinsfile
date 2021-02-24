String cron_string = BRANCH_NAME == "master" ? "@hourly" : BRANCH_NAME.startsWith("PR") ? "@midnight" : ""

pipeline {
    agent { label 'centos_worker' }
    triggers { cron(cron_string) }
    environment {
        PATH = "${PATH}:/usr/local/go/bin"
        BUILD_TYPE = "CI"

        // Images
        ASSISTED_ORG = "quay.io/ocpmetal"
        ASSISTED_TAG = "${BUILD_TAG}"

        // Credentials
        SLACK_TOKEN = credentials('slack-token')
        QUAY_IO_CREDS = credentials('ocpmetal_cred')
        TWINE_CREDS = credentials('assisted-pypi')

        TWINE_USERNAME="${TWINE_CREDS_USR}"
        TWINE_PASSWORD="${TWINE_CREDS_PSW}"
        TWINE_REPOSITORY="pypi"
    }
    options {
      timeout(time: 2, unit: 'HOURS')
    }

    stages {
        stage('Init') {
            steps {
                sh 'make clear-all || true'
                sh 'make ci-lint'

                // Login to quay.io
                sh "docker login quay.io -u ${QUAY_IO_CREDS_USR} -p ${QUAY_IO_CREDS_PSW}"
                sh "podman login quay.io -u ${QUAY_IO_CREDS_USR} -p ${QUAY_IO_CREDS_PSW}"
            }
        }

        stage('Build') {
            steps {
                sh "make build-all"
                sh "make jenkins-deploy-for-subsystem"
                sh "kubectl get pods -A"
            }
            post {
                always {
                    junit '**/reports/*test.xml'
                    cobertura coberturaReportFile: '**/reports/*coverage.xml', onlyStable: false, enableNewApi: true
                }
            }
        }

        stage('Subsystem Test') {
            steps {
                sh "make subsystem-run"
            }
        }

        stage('Publish') {
            when {
                expression {!env.BRANCH_NAME.startsWith('PR')}
            }
            steps {
                sh "make publish"
            }
        }

        stage('Publish master') {
            when { branch 'master'}
            steps {
                sh "make publish PUBLISH_TAG=latest"
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

                sh "kubectl get all -A"

                for (service in ["assisted-service","postgres","scality","createimage"]) {
                    sh "kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep ${service} | xargs -r -I {} sh -c \"kubectl logs {} -n assisted-installer > k8s_{}.log\" || true"
                }

                sh "make clear-all"

                archiveArtifacts artifacts: '*.log', fingerprint: true
            }
        }
    }
}
