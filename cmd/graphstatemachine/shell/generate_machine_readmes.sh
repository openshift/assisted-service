for machine in ${machines[@]}; do
    JSON=$machine
    OUT_DIR=${SCRIPT_DIR}/doc/$(basename $machine .json)

    mkdir --parents "${OUT_DIR}"/
    mkdir --parents "${OUT_DIR}"/media

    OUT_FILE="${OUT_DIR}"/README.md

    # Import helper functions
    . "${SHELL_DIR}"/generate_machine_readme.sh &
done

wait
