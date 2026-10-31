process COLLIGO_CHECK {
    tag "${meta.id}"
    label 'process_single'

    conda "${moduleDir}/environment.yml"

    input:
    tuple val(meta), path(reads)

    output:
    tuple val(meta), path("*.colligo.json"), emit: json
    path "versions.yml"                    , emit: versions, topic: versions

    when:
    task.ext.when == null || task.ext.when

    script:
    def args = task.ext.args ?: ''
    def prefix = task.ext.prefix ?: "${meta.id}"
    """
    colligo check \\
        --json \\
        --jobs ${task.cpus} \\
        ${args} \\
        ${reads} \\
        > ${prefix}.colligo.json

    cat <<-END_VERSIONS > versions.yml
    "${task.process}":
        colligo: \$(colligo version | sed 's/^colligo v//; s/ .*//')
    END_VERSIONS
    """

    stub:
    def prefix = task.ext.prefix ?: "${meta.id}"
    """
    echo '{"schema_version": 2, "files": [], "exit_code": 0}' > ${prefix}.colligo.json

    cat <<-END_VERSIONS > versions.yml
    "${task.process}":
        colligo: \$(colligo version | sed 's/^colligo v//; s/ .*//')
    END_VERSIONS
    """
}
