defmodule SlimServe.S3HelmTest do
  # Needs `helm` on PATH: CI installs it and runs `mix test --only helm`.
  use ExUnit.Case, async: true

  import Plug.Conn

  @moduletag :helm
  @moduletag :tmp_dir

  @host ["--set-string", "host=files.example.test"]
  @s3 ["--set-string", "storage.backend=s3,s3.bucket=files,s3.accessKeyId=id,s3.secretAccessKey=secret"]
  @cluster [
    "--set",
    "replicas=2",
    "--set-string",
    "cluster.cookie=cookie,auth.secretKeyBase=#{String.duplicate("k", 64)}"
  ]

  defp helm(tmp_dir, args) do
    env = for name <- ~w(HELM_CACHE_HOME HELM_CONFIG_HOME HELM_DATA_HOME), do: {name, tmp_dir}

    System.cmd("helm", ["template", "files", "manifests/helm", "-n", "files" | @host ++ args],
      env: env,
      stderr_to_stdout: true
    )
  end

  defp render!(tmp_dir, args) do
    {out, 0} = helm(tmp_dir, args)
    out |> String.split(~r/^---\n/m, trim: true) |> Map.new(&{&1 |> String.split("\n", parts: 2) |> hd(), &1})
  end

  test "S3 leaves the data claim exactly as local mode renders it, and does not mount /data", %{tmp_dir: tmp_dir} do
    local = render!(tmp_dir, ["--is-upgrade"])
    s3 = render!(tmp_dir, ["--is-upgrade" | @s3])
    pvc = "# Source: slimserve/templates/pvc.yml"

    assert local[pvc] =~ "kind: PersistentVolumeClaim"
    assert s3[pvc] == local[pvc]

    deployment = "# Source: slimserve/templates/deployment.yml"
    assert local[deployment] =~ "mountPath: /data"
    refute s3[deployment] =~ "/data"
    assert s3[deployment] =~ "mountPath: /cache"
  end

  test "Scenario: Replicas cluster only over S3, with a shared cookie and signing key, beside the HTTP Service", %{
    tmp_dir: tmp_dir
  } do
    assert {out, 1} = helm(tmp_dir, ["--set", "replicas=2", "--set-string", "cluster.cookie=c,auth.secretKeyBase=k"])
    assert out =~ "replicas above 1 need storage.backend: s3"
    assert {out, 1} = helm(tmp_dir, @s3 ++ ["--set", "replicas=2"])
    assert out =~ "replicas above 1 need cluster.cookie and auth.secretKeyBase"

    docs = render!(tmp_dir, @s3 ++ @cluster)
    assert docs["# Source: slimserve/templates/deployment.yml"] =~ "value: slimserve@$(POD_IP)"
    assert docs["# Source: slimserve/templates/headless.yml"] =~ "clusterIP: None"
    refute docs["# Source: slimserve/templates/service.yml"] =~ "4369"
    refute Map.has_key?(render!(tmp_dir, @s3), "# Source: slimserve/templates/headless.yml")
  end

  describe "an upgrade previewed against a cluster" do
    setup %{tmp_dir: tmp_dir} do
      claim? = start_supervised!({Agent, fn -> true end})

      server =
        start_supervised!(
          {Bandit, plug: {__MODULE__, %{test: self(), claim?: claim?}}, ip: :loopback, port: 0, startup_log: false}
        )

      {:ok, {_ip, port}} = ThousandIsland.listener_info(server)

      kubeconfig = Path.join(tmp_dir, "kubeconfig")

      File.write!(kubeconfig, """
      apiVersion: v1
      kind: Config
      clusters: [{name: fixture, cluster: {server: "http://127.0.0.1:#{port}"}}]
      users: [{name: fixture, user: {token: fixture}}]
      contexts: [{name: fixture, context: {cluster: fixture, user: fixture}}]
      current-context: fixture
      """)

      {:ok, claim?: claim?, preview: ["--is-upgrade", "--dry-run=server", "--kubeconfig", kubeconfig]}
    end

    test "still refuses to drop an existing data claim when switching to S3", %{
      tmp_dir: tmp_dir,
      claim?: claim?,
      preview: preview
    } do
      drop = preview ++ @s3 ++ ["--set", "persistence.enabled=false"]

      assert {out, 1} = helm(tmp_dir, drop)
      assert out =~ "cannot disable persistence on an upgrade with an existing data claim"
      assert_received {:get, "/api/v1/namespaces/files/persistentvolumeclaims/files-data-pvc"}

      Agent.update(claim?, fn _ -> false end)
      assert {_out, 0} = helm(tmp_dir, drop)
    end
  end

  def init(opts), do: opts

  def call(%{method: "GET", request_path: path} = conn, %{test: test, claim?: claim?}) do
    send(test, {:get, path})

    case path do
      "/version" ->
        json(conn, 200, %{major: "1", minor: "33", gitVersion: "v1.33.0"})

      "/api" ->
        json(conn, 200, %{kind: "APIVersions", versions: ["v1"], serverAddressByClientCIDRs: []})

      "/apis" ->
        apps = %{groupVersion: "apps/v1", version: "v1"}

        json(conn, 200, %{
          kind: "APIGroupList",
          apiVersion: "v1",
          groups: [%{name: "apps", versions: [apps], preferredVersion: apps}]
        })

      "/api/v1" ->
        json(
          conn,
          200,
          resources("v1", [
            {"persistentvolumeclaims", "PersistentVolumeClaim"},
            {"secrets", "Secret"},
            {"services", "Service"}
          ])
        )

      "/apis/apps/v1" ->
        json(conn, 200, resources("apps/v1", [{"deployments", "Deployment"}]))

      "/api/v1/namespaces/files/persistentvolumeclaims/files-data-pvc" ->
        if Agent.get(claim?, & &1),
          do:
            json(conn, 200, %{
              apiVersion: "v1",
              kind: "PersistentVolumeClaim",
              metadata: %{name: "files-data-pvc", namespace: "files"}
            }),
          else: not_found(conn)

      _other ->
        not_found(conn)
    end
  end

  defp resources(group_version, kinds) do
    resources =
      for {name, kind} <- kinds, do: %{name: name, singularName: "", namespaced: true, kind: kind, verbs: ["get", "list"]}

    %{kind: "APIResourceList", apiVersion: "v1", groupVersion: group_version, resources: resources}
  end

  defp not_found(conn),
    do: json(conn, 404, %{kind: "Status", apiVersion: "v1", status: "Failure", reason: "NotFound", code: 404})

  defp json(conn, status, body),
    do: conn |> put_resp_content_type("application/json") |> send_resp(status, Jason.encode!(body))
end
