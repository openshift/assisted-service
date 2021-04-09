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

                // Logout from problematic registries
                sh 'docker logout registry.svc.ci.openshift.org || true'
                sh 'podman logout --all || true'
                sh 'oc logout || true'

                // Login to quay.io
                sh "docker login quay.io -u ${QUAY_IO_CREDS_USR} -p ${QUAY_IO_CREDS_PSW}"
                sh "podman login quay.io -u ${QUAY_IO_CREDS_USR} -p ${QUAY_IO_CREDS_PSW}"

                sh 'make ci-lint'
            }
        }

        stage('Build') {
            steps {
                sh "make build-all"
            }
        }

        stage('Publish') {
            when {
                expression {!env.BRANCH_NAME.startsWith('PR')}
            }
            steps {
                sh "make publish"
                // When the index index is being built, the opm tooling pulls the bundle
                // image from quay, so the index is built after the bundle image has been
                // published.
                sh "make build-publish-index"
            }
        }

        stage('Publish master') {
            when { branch 'master'}
            steps {
                sh "make publish PUBLISH_TAG=latest"
                // When the index index is being built, the opm tooling pulls the bundle
                // image from quay, so the index is built after the bundle image has been
                // published.
                sh "make build-publish-index PUBLISH_TAG=latest"
            }
        }
    }

    post {
        always {
            script {
                if ((env.BRANCH_NAME == 'master') && (currentBuild.currentResult == "ABORTED" || currentBuild.currentResult == "FAILURE")){
                    script {
                        def data = [text: "Attention! ${BUILD_TAG} job ${currentBuild.currentResult}, see: ${BUILD_URL}"]
                        writeJSON(file: 'data.txt', json: data, pretty: 4)
                    }

                    sh '''curl -X POST -H 'Content-type: application/json' --data-binary "@data.txt" https://hooks.slack.com/services/${SLACK_TOKEN}'''
                }

                archiveArtifacts artifacts: '*.log', fingerprint: true, allowEmptyArchive: true
                junit '**/reports/junit*.xml'
                cobertura coberturaReportFile: '**/reports/*coverage.xml', onlyStable: false, enableNewApi: true

                sh "make clear-all"
            }
        }
    }
}
