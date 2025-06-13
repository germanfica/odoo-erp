def buildTag = "1.0.${BUILD_NUMBER}"
def buildBranch = 'main'
// Jenkins aún no ha definido env.WORKSPACE, por lo que no se puede usar aquí
// def customWorkspace = "${env.WORKSPACE}/${env.JOB_NAME}"

pipeline {
    //agent any
    agent { label 'build server' }
    // agent {
    //     node {
    //         label 'build server'
    //         //customWorkspace "${customWorkspace}"
    //         customWorkspace "${env.WORKSPACE}/${env.JOB_NAME}"
    //     }
    // }
    tools { nodejs 'v22.14.0' }  // Nombre configurado en Jenkins // esto genera un worskpace extra

    environment {
        GITHUB_SSH_CREDENTIALS_ID = 'a1144af3-f687-4c8e-9963-5acdee26090a' // Reemplaza con el ID de tus credenciales SSH de tipo Username with private key
        SSH_KEY=credentials('adcc6d4c-e160-4bc5-ad33-6911e6fc152f') // Secret kind: SSH Username with private key
        REPO_URL = 'git@github.com:germanfica/odoo-erp.git'
        PROJECT_NAME = 'odoo'
        APP_IMAGE_NAME = 'odoo'
        APP_IMAGE_TAG = '18'
        INVENTORY_PATH = '/opt/ansible-infra/inventories/production/applications/odoo/inventory.yml'
        PLAYBOOK_PATH = '/opt/ansible-infra/playbooks/deploy-docker-app.yml'
        // Nota: WORKSPACE es una variable de Jenkins, no una variable de Groovy. No usar comillas simples en TEMPLATE_SRC porque Groovy no hace interpolación en ellas.
        ODOO_TEMPLATE_SRC = "${WORKSPACE}/templates/docker-compose.odoo.yml.j2"
        TARGET_HOSTS = 'localhost'
        LIMIT_HOSTS = 'localhost'
        USE_BECOME = 'true'
        POSTGRES_VERSION = "16"
    }

    options {
        disableConcurrentBuilds()
        //throttleJobProperty(maxConcurrentPerNode: 1, maxConcurrentTotal: 1) // DON'T USE
        rateLimitBuilds(throttle: [count: 1, durationName: 'minute'])
    }

    stages {
        stage('Setup parameters') {
            steps {
                script {
                    properties([
                        parameters([
                            // choice(
                            //     choices: [],
                            //     name: 'GIT_TAG'
                            // ),
                            choice(
                                choices: ['tag', 'main'],
                                name: 'GIT_BRANCH'
                            ),
                            gitParameter (
                                branch: buildBranch, branchFilter: '.*', defaultValue: '',
                                description: 'Select a git tag to use in this build. This parameter requires the git-parameter plugin.',
                                name: 'GIT_TAG', quickFilterEnabled: false, selectedValue: 'NONE', sortMode: 'NONE', tagFilter: '*', type: 'PT_TAG'
                            )
                        ])
                    ])
                }
            }
        }

        stage('Check parameters') {
            steps {
                script {
                    if(params.GIT_BRANCH.toString().equals('tag')){
                        if(params.GIT_TAG == null || params.GIT_TAG.toString().isEmpty()) {
                            currentBuild.result = 'ABORTED';
                            error("GIT_TAG is empty")
                        }else {
                            buildTag = params.GIT_TAG.toString()
                            buildBranch = params.GIT_TAG.toString()
                        }
                    }
                    if(params.GIT_BRANCH == null || params.GIT_BRANCH.toString().isEmpty()) {
                        currentBuild.result = 'ABORTED';
                        error("GIT_BRANCH is empty")
                    }
                    echo "Build tag: ${buildTag}"
                }
            }
        }

        stage('Checkout') {
            steps {
                checkout([$class: 'GitSCM', branches: [[name: buildBranch]],
                          doGenerateSubmoduleConfigurations: false,
                          extensions: [],
                          gitTool: 'Default',
                          userRemoteConfigs: [[url: env.REPO_URL, credentialsId: env.GITHUB_SSH_CREDENTIALS_ID]]])
                echo 'Checkout completed. Proceeding to the next stage.'
            }
        }

        stage('Deploy odoo App') {
            steps {
                script {
                    sh 'ls -la'
                    withCredentials([file(credentialsId: 'ansible-vault-password', variable: 'VAULT_PASS_FILE')]) {
                        withEnv(["BUILD_TAG=${buildTag}"]) {
                            sh 'ansible-playbook -i $INVENTORY_PATH --private-key=$SSH_KEY --vault-password-file="$VAULT_PASS_FILE" $PLAYBOOK_PATH --limit $LIMIT_HOSTS --extra-vars "APP_VERSION=$APP_IMAGE_TAG APP_IMAGE_NAME=$APP_IMAGE_NAME APP_NAME=$PROJECT_NAME WORKSPACE=$WORKSPACE TEMPLATE_SRC=$ODOO_TEMPLATE_SRC POSTGRES_VERSION=$POSTGRES_VERSION USE_TAR=false TARGET_HOSTS=$TARGET_HOSTS USE_BECOME=$USE_BECOME"'
                        }
                    }
                }
            }
        }
    }
    // esto genera un worskpace extra
    post {
        success {
            echo "Current workspace: ${env.WORKSPACE}"
            echo "✅ Deployment completed successfully."
        }
        failure {
            echo "Current workspace: ${env.WORKSPACE}"
            echo "❌ Deployment failed. Please check logs."
        }
    }
}
