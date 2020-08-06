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

		stage('Deploy for subsystem') {
			steps {
				script {
					docker.withRegistry('https://docker.io/', 'dockerio_cred') {
						sh '''export PATH=$PATH:/usr/local/go/bin; make jenkins-deploy-for-subsystem'''
						sleep 60
						sh '''# Dump pod statuses;kubectl  get pods -A'''
				}
				}
			}
		}

		stage('Subsystem-test') {
			steps {
				sh '''export PATH=$PATH:/usr/local/go/bin;make subsystem-run'''
			}
		}

		stage('publish images on push to master') {
			when {
				branch 'master'
			}
			steps {
				script {
					docker.withRegistry('https://quay.io/', 'ocpmetal_cred') {
						def img = docker.image(${SERVICE})
						img.push('latest')
						img.push('${GIT_COMMIT}')
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
