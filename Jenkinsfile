// Get current date
def now = new Date()

// isTriggerByTimer == true if the timer triggers it
// isEmpty() on lists is currently broken in Jenkins pipelines... so we have to rely on the size of the list
boolean isTriggeredByTimer = currentBuild.getBuildCauses('hudson.triggers.TimerTrigger$TimerTriggerCause').size() != 0

// Generate the cron string based off the branch name
def cronScheduleString(branchName = BRANCH_NAME) {
    String cronScheduleString
    if (isCandidateBranch(branchName)) {
        cronScheduleString = '@hourly'
    } else if (branchName ==~ /^PR.*/) {
        cronScheduleString = '@midnight'
    } else {
        cronScheduleString = ''
    }
    return cronScheduleString
}

// Determine if the branch is a branch we want to create release candidate images from
def isCandidateBranch(branchName = BRANCH_NAME) {
    // List of regex to match branches for release candidate publishing
    def candidateBranches = [/^master$/, /^ocm-\d[.]{1}\d$/]
    return branchName && (candidateBranches.collect { branchName =~ it ? true : false }).contains(true)
}

// Determine the publish tag for the release candidate images
def releaseBranchPublishTag(branchName = BRANCH_NAME) {
    String publish_tag
    if (branchName == 'master') {
        publish_tag = 'latest'
    } else {
        publish_tag = branchName
    }
    return publish_tag
}

pipeline {
    agent { label 'centos_worker' }
    triggers { cron(cronScheduleString(env.BRANCH_NAME)) }
    environment {
        PATH = "${PATH}:/usr/local/go/bin"
        BUILD_TYPE = "CI"

        CURRENT_DATE = now.format("Ymd")
        CURRENT_HOUR = now.format("H")
        PUBLISH_TAG = releaseBranchPublishTag(env.BRANCH_NAME)

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

        stage('Publish Release Candidates') {
            when { 
                expression { isCandidateBranch(env.BRANCH_NAME) }
            }
            steps {
                sh "make publish PUBLISH_TAG=${PUBLISH_TAG}"
                // When the index index is being built, the opm tooling pulls the bundle
                // image from quay, so the index is built after the bundle image has been
                // published.
                sh "make build-publish-index PUBLISH_TAG=${PUBLISH_TAG}"

                // Release a nightly build at midnight for ocm branches
                script {
                    if (env.BRANCH_NAME ==~ /^ocm-\d[.]{1}\d$/ && isTriggeredByTimer && env.CURRENT_HOUR == '0') {
                        sh "make publish PUBLISH_TAG=${PUBLISH_TAG}-${CURRENT_DATE}"
                        sh "make build-publish-index PUBLISH_TAG=${PUBLISH_TAG}-${CURRENT_DATE}"
                    }
                }
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

                    sh '''curl --retry 5 -X POST -H 'Content-type: application/json' --data-binary "@data.txt" https://hooks.slack.com/services/${SLACK_TOKEN}'''
                }

                archiveArtifacts artifacts: '*.log', fingerprint: true, allowEmptyArchive: true
                junit '**/reports/junit*.xml'
                cobertura coberturaReportFile: '**/reports/*coverage.xml', onlyStable: false, enableNewApi: true

                sh "make clear-all"
            }
        }
    }
}
