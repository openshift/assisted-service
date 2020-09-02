String cron_string = BRANCH_NAME == "master" ? "@hourly" : ""

pipeline {
    agent { label 'centos_worker' }
    triggers { cron(cron_string) }
    environment {
        PATH = "${PATH}:/usr/local/go/bin"

        // Images
        SERVICE = "quay.io/ocpmetal/assisted-service:${JOB_BASE_NAME}"
        ISO_CREATION = "quay.io/ocpmetal/assisted-iso-create:${JOB_BASE_NAME}"

        // Credentials
        SLACK_TOKEN = credentials('slack-token')
        QUAY_IO_CREDS = credentials('ocpmetal_cred')
        DOCKER_IO_CREDS = credentials('dockerio_cred')
    }
    options {
      timeout(time: 1, unit: 'HOURS')
    }

    stages {
        stage('Build') {
            steps {
                sh "docker login quay.io -u ${QUAY_IO_CREDS_USR} -p ${QUAY_IO_CREDS_PSW}"
                sh "make update"
            }
        }

        stage('Test') {
            parallel {
                stage('Test on k8s') {
                    agent { label 'centos_worker' }
                    steps {
                        check_if_minikube_is_running()
                        clear_slave()

                        sh '''docker login docker.io -u ${DOCKER_IO_CREDS_USR} -p ${DOCKER_IO_CREDS_PSW}'''
                        sh '''make jenkins-deploy-for-subsystem'''
                        sh '''kubectl get pods -A'''
                        sh '''make subsystem-run'''
                    }
                    post {
                        always {
                            script {
                                for (pod in ["assisted-service", "postgres", "scality", "createimage"]) {
                                    sh '''
                                        echo "Get ${pod} log"
                                        kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep ${pod} | xargs -r -I {} sh -c "kubectl logs {} -n assisted-installer > ${WORKSPACE}/${pod}.log" || true
                                    '''
                                }
                            }
                            clear_slave()
                        }
                    }
                }
                stage('Test onprem') {
                    agent { label 'centos_worker' }
                    steps {
                        clear_slave()

                        sh '''make build-onprem'''
                        sh '''docker login docker.io -u ${DOCKER_IO_CREDS_USR} -p ${DOCKER_IO_CREDS_PSW}'''
                        sh '''make deploy-onprem'''
                        sh '''podman ps'''
                        sh '''make test-onprem'''
                    }
                    post {
                        always {
                            script {
                                for (pod in ["assisted-service", "postgres"]) {
                                    sh '''
                                        echo "Get ${pod} log"
                                        kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep ${pod} | xargs -r -I {} sh -c "kubectl logs {} -n assisted-installer > ${WORKSPACE}/${pod}.log" || true
                                    '''
                                }
                            }
                            clear_slave()
                        }
                    }
                }
            }
        }

        stage('Publish') {
            when { branch 'master' }
            steps {
                sh '''docker tag ${SERVICE} quay.io/ocpmetal/assisted-service:latest'''
                sh '''docker tag ${SERVICE} quay.io/ocpmetal/assisted-service:${GIT_COMMIT}'''
                sh '''docker push quay.io/ocpmetal/assisted-service:latest'''
                sh '''docker push quay.io/ocpmetal/assisted-service:${GIT_COMMIT}'''

                sh '''docker tag ${ISO_CREATION} quay.io/ocpmetal/assisted-iso-create:latest'''
                sh '''docker tag ${ISO_CREATION} quay.io/ocpmetal/assisted-iso-create:${GIT_COMMIT}'''
                sh '''docker push quay.io/ocpmetal/assisted-iso-create:latest'''
                sh '''docker push quay.io/ocpmetal/assisted-iso-create:${GIT_COMMIT}'''
            }
        }
    }
    post {
        failure {
            when { branch 'master' }
            script {
                def data = [text: "Attention! ${BUILD_TAG} job failed, see: ${BUILD_URL}"]
                writeJSON(file: 'data.txt', json: data, pretty: 4)
                sh '''curl -X POST -H 'Content-type: application/json' --data-binary "@data.txt"  https://hooks.slack.com/services/${SLACK_TOKEN}'''
            }
        }
    }
}

void check_if_minikube_is_running() {
    sh '''minikube delete'''
    sh '''minikube start --driver=none'''
    sh '''
        if [ $(minikube status|grep Running)="" ] ; then
            echo "minikube is not running on $NODE_NAME, failing job BUILD_URL"
            echo '{"text":"minikube is not running on: ' > minikube_data.txt
            echo ${NODE_NAME} >> minikube_data.txt
            echo 'failing job: ' >> minikube_data.txt
            echo ${BUILD_URL} >> minikube_data.txt
            echo '"}' >> minikube_data.txt
            curl -X POST -H 'Content-type: application/json' --data-binary "@minikube_data.txt"  https://hooks.slack.com/services/$SLACK_TOKEN
            sh "exit 1"
        fi
    '''
}

void clear_slave(){
    sh 'podman pod rm -f assisted-installer || true'
    sh 'make clear-deployment'
    sh 'podman image prune -a'
    sh 'docker image prune -a -f'
}
