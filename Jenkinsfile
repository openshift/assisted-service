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

		stage('Build') {
			steps {
				sh '''export PATH=$PATH:/usr/local/go/bin; make build-image'''
			}
		}


		stage('publish images on push to master') {
			steps {
				script {
					docker.withRegistry('https://quay.io/', 'ocpmetal_cred') {
						try {
							def img = docker.image(${SERVICE})
								img.push('latest')
								img.push('${GIT_COMMIT}')
						} catch(e) {
							throw e
						}

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
