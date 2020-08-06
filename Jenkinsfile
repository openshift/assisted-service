pipeline {
	agent { label 'bm-inventory-subsystem' }
	environment {
		SERVICE = 'ocpmetal/assisted-service'
	}

	stages {
		stage('clear deployment') {
			steps {
				sh 'make clear-deployment'
			}
		}


		stage('publish images on push to master') {
			steps {
				script {
					echo "hello world 1"
						try {
							echo "hello world 2"
								docker.withRegistry('https://quay.io/', 'ocpmetal_cred') {
								echo "hello world 3: $SERVICE"
								def img = docker.image('${SERVICE}')
								echo "hello world 4"
								img.push('latest')
								echo "hello world 5"
								img.push('${GIT_COMMIT}')
								echo "hello world 6"
						}
						} catch(Exception e) {
							echo "Exception thrown:\n ${e}"
								throw e
						}

				}
			}
		}
	}

	post {
		failure {
			echo 'Get assisted-service log'
				sh '''
				kubectl get pods -o=custom-columns=NAME:.metadata.name -A | grep assisted-service | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
				mv test_dd.log $WORKSPACE/assisted-service.log || true
				'''

				echo 'Get postgres log'
				sh '''kubectl  get pods -o=custom-columns=NAME:.metadata.name -A | grep postgres | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
				mv test_dd.log $WORKSPACE/postgres.log || true
				'''

				echo 'Get scality log'
				sh '''kubectl  get pods -o=custom-columns=NAME:.metadata.name -A | grep scality | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
				mv test_dd.log $WORKSPACE/scality.log || true
				'''

				echo 'Get createimage log'
				sh '''kubectl  get pods -o=custom-columns=NAME:.metadata.name -A | grep createimage | xargs -I {} sh -c "kubectl logs {} -n  assisted-installer > test_dd.log"
				mv test_dd.log $WORKSPACE/createimage.log || true
				'''
		}
	}
}
