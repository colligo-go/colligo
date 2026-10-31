process COLLIGO_CHECK {
    tag "${meta.id}"
    label 'process_single'

    conda "${moduleDir}/environment.yml"

    input:
    tuple val(meta), path(reads)

    output:
    tuple val(meta), path("*.colligo.json")     , emit: json
    tuple val(meta), path("*.colligo.exit_code"), emit: exit_code
    path "versions.yml"                         , emit: versions, topic: versions

    when:
    task.ext.when == null || task.ext.when

    script:
    def args = task.ext.args ?: ''
    def prefix = task.ext.prefix ?: "${meta.id}"
    def fail_on_blocking = task.ext.fail_on_blocking ?: false
    """
    status=0
    colligo check \\
        --json \\
        --jobs ${task.cpus} \\
        ${args} \\
        ${reads} \\
        > ${prefix}.colligo.json || status=\$?

    echo \$status > ${prefix}.colligo.exit_code

    if [ \$status -ne 0 ] && { [ \$status -ne 3 ] || [ "${fail_on_blocking}" = "true" ]; }; then
        exit \$status
    fi

    cat <<-END_VERSIONS > versions.yml
    "${task.process}":
        colligo: \$(colligo version | sed 's/^colligo v//; s/ .*//')
    END_VERSIONS
    """

    stub:
    def prefix = task.ext.prefix ?: "${meta.id}"
    """
    echo '{"schema_version": 2, "files": [], "exit_code": 0}' > ${prefix}.colligo.json
    echo 0 > ${prefix}.colligo.exit_code

    cat <<-END_VERSIONS > versions.yml
    "${task.process}":
        colligo: \$(colligo version | sed 's/^colligo v//; s/ .*//')
    END_VERSIONS
    """
}
