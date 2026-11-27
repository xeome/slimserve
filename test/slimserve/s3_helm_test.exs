defmodule SlimServe.S3HelmTest do
  # Needs `helm` on PATH: CI installs it and runs `mix test --only helm`.
  use ExUnit.Case, async: true

  import Plug.Conn

  @moduletag :helm
  @moduletag :tmp_dir

  @host ["--set-string", "host=files.example.test"]
  @s3 ["--set-string", "storage.backend=s3,s3.bucket=files,s3.accessKeyId=id,s3.secretAccessKey=secret"]
  @keys ["--set-string", "cluster.cookie=cookie,auth.secretKeyBase=#{String.duplicate("k", 64)}"]
  @cluster ["--set", "replicas=2" | @keys]

  @deployment "# Source: slimserve-helm/templates/deployment.yml"
  @headless "# Source: slimserve-helm/templates/headless.yml"
  @service "# Source: slimserve-helm/templates/service.yml"
  @secret "# Source: slimserve-helm/templates/secret.yml"
  @pvc "# Source: slimserve-helm/templates/pvc.yml"
  @pdb "# Source: slimserve-helm/templates/pdb.yml"
  @network_policy "# Source: slimserve-helm/templates/networkpolicy.yml"
  @service_account "# Source: slimserve-helm/templates/serviceaccount.yml"

  @rollout """
    minReadySeconds: 10
    strategy:
      type: RollingUpdate
      rollingUpdate:
        maxSurge: 1
        maxUnavailable: 0
  """

  @pin """
          podAffinity:
            requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: files-main
              topologyKey: kubernetes.io/hostname
  """

  @zone_term """
          podAntiAffinity:
            preferredDuringSchedulingIgnoredDuringExecution:
            - podAffinityTerm:
                labelSelector:
                  matchLabels:
                    app: files-main
                topologyKey: topology.kubernetes.io/zone
              weight: 100
  """

  @cluster_env """
              - name: POD_IP
                valueFrom:
                  fieldRef:
                    fieldPath: status.podIP
              - name: RELEASE_DISTRIBUTION
                value: name
              - name: RELEASE_NODE
                value: slimserve@$(POD_IP)
              - name: DNS_CLUSTER_QUERY
                value: "files-nodes.files.svc.cluster.local"
  """

  @credential_keys ~w(SLIMSERVE_PASSWORD SLIMSERVE_WRITE_PASSWORD SECRET_KEY_BASE AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY RELEASE_COOKIE)

  defp helm(tmp_dir, args, command \\ ["template", "files", "manifests/helm"]) do
    env = for name <- ~w(HELM_CACHE_HOME HELM_CONFIG_HOME HELM_DATA_HOME), do: {name, tmp_dir}

    System.cmd("helm", command ++ ["-n", "files" | @host ++ args],
      env: env,
      stderr_to_stdout: true
    )
  end

  defp render!(tmp_dir, args) do
    assert {out, 0} = helm(tmp_dir, args)
    out |> String.split(~r/^\n?---\n/m, trim: true) |> Map.new(&{&1 |> String.split("\n", parts: 2) |> hd(), &1})
  end

  defp refused(tmp_dir, args) do
    assert {out, 1} = helm(tmp_dir, args)
    out
  end

  defp secret_env(required) do
    Enum.map_join(@credential_keys, fn key ->
      """
                  - name: #{key}
                    valueFrom:
                      secretKeyRef:
                        name: "creds"
                        key: #{key}
                        optional: #{key not in required}
      """
    end)
  end

  test "S3 with default values renders no data claim and does not mount /data", %{tmp_dir: tmp_dir} do
    local = render!(tmp_dir, ["--is-upgrade"])
    s3 = render!(tmp_dir, ["--is-upgrade", "--set", "replicas=1" | @s3])

    assert local[@pvc] =~ "kind: PersistentVolumeClaim"
    assert local[@deployment] =~ "mountPath: /data\n              subPath: subpath\n"
    assert local[@deployment] =~ ~s(claimName: "files-data-pvc")
    refute Map.has_key?(s3, @pvc)

    refute s3[@deployment] =~ "/data"
    assert s3[@deployment] =~ "mountPath: /cache"
  end

  test "S3 with persistence.enabled=true renders the data claim exactly as local mode does", %{tmp_dir: tmp_dir} do
    local = render!(tmp_dir, ["--is-upgrade"])
    s3 = render!(tmp_dir, ["--is-upgrade", "--set", "replicas=1,persistence.enabled=true" | @s3])

    assert local[@pvc] =~ "kind: PersistentVolumeClaim"
    assert s3[@pvc] == local[@pvc]
    refute s3[@deployment] =~ "/data"
  end

  test "Scenario: Replicas cluster on either storage, with a shared cookie and signing key, beside the HTTP Service", %{
    tmp_dir: tmp_dir
  } do
    assert refused(tmp_dir, ["--set", "replicas=3,persistence.enabled=false" | @keys]) =~ "persistence.enabled"

    disk = render!(tmp_dir, ["--set", "replicas=3" | @keys])
    assert disk[@deployment] =~ @cluster_env
    assert disk[@deployment] =~ "      affinity:\n" <> @pin
    assert disk[@headless] =~ "clusterIP: None"

    assert refused(tmp_dir, @s3 ++ ["--set", "replicas=3"]) =~ "cluster.cookie"
    assert refused(tmp_dir, @s3 ++ ["--set", "replicas=3", "--set-string", "cluster.cookie=c"]) =~ "auth.secretKeyBase"

    s3 = render!(tmp_dir, @s3 ++ ["--set", "replicas=3" | @keys])
    assert s3[@deployment] =~ @cluster_env
    assert s3[@headless] =~ "clusterIP: None"

    for docs <- [disk, s3] do
      refute docs[@service] =~ "4370"
      refute docs[@service] =~ "dist"
    end

    refute Map.has_key?(render!(tmp_dir, @s3 ++ ["--set", "replicas=1"]), @headless)
  end

  test "one replica never clusters, on either storage", %{tmp_dir: tmp_dir} do
    for shape <- [[], @s3] do
      docs = render!(tmp_dir, shape ++ ["--set", "replicas=1" | @keys])

      refute Map.has_key?(docs, @headless)
      refute Map.has_key?(docs, @network_policy)
      refute docs[@deployment] =~ "RELEASE_DISTRIBUTION"
    end
  end

  test "a numeric credential set with --set reaches the Secret unchanged", %{tmp_dir: tmp_dir} do
    docs =
      render!(tmp_dir, [
        "--set",
        "auth.username=1234,auth.password=12345678,cluster.cookie=123456",
        "--set-string",
        "auth.secretKeyBase=#{String.duplicate("k", 64)}"
      ])

    [_metadata, data] = String.split(docs[@secret], "\ndata:\n")
    secret = for [_, key, value] <- Regex.scan(~r/^  (\w+): (\S+)$/m, data), into: %{}, do: {key, Base.decode64!(value)}

    assert secret["SLIMSERVE_PASSWORD"] == "12345678"
    assert secret["RELEASE_COOKIE"] == "123456"
    assert docs[@deployment] =~ ~s(- name: SLIMSERVE_USERNAME\n              value: "1234"\n)
  end

  describe "local storage" do
    test "rolls one pod at a time, pinned to the disk's node, with no PDB and no NetworkPolicy", %{tmp_dir: tmp_dir} do
      for replicas <- ["1", "3"] do
        docs = render!(tmp_dir, ["--set", "replicas=#{replicas}" | @keys])
        assert docs[@deployment] =~ "  replicas: #{replicas}\n" <> @rollout
        assert docs[@deployment] =~ "      affinity:\n" <> @pin
        refute Map.has_key?(docs, @pdb)
      end

      docs = render!(tmp_dir, [])
      assert docs[@deployment] =~ "  replicas: 1\n"
      refute Map.has_key?(docs, @network_policy)
    end

    test "never clusters one replica, even with a cookie and an existing Secret", %{tmp_dir: tmp_dir} do
      docs = render!(tmp_dir, ["--set", "replicas=1", "--set-string", "cluster.cookie=c,existingSecret=creds"])

      refute Map.has_key?(docs, @headless)
      refute docs[@deployment] =~ "POD_IP"
      refute docs[@deployment] =~ "4370"
    end

    test "keeps an operator's nodeAffinity beside the pin", %{tmp_dir: tmp_dir} do
      node_affinity =
        ~s({"nodeAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":{"nodeSelectorTerms":[{"matchExpressions":[{"key":"disk","operator":"In","values":["ssd"]}]}]}}})

      docs = render!(tmp_dir, ["--set-json", "affinity=#{node_affinity}"])

      assert docs[@deployment] =~
               """
                     affinity:
                       nodeAffinity:
                         requiredDuringSchedulingIgnoredDuringExecution:
                           nodeSelectorTerms:
                           - matchExpressions:
                             - key: disk
                               operator: In
                               values:
                               - ssd
               """ <> @pin
    end

    test "refuses an operator podAffinity, and replicas without a persistent volume", %{tmp_dir: tmp_dir} do
      pod_affinity =
        ~s({"podAffinity":{"requiredDuringSchedulingIgnoredDuringExecution":[{"labelSelector":{"matchLabels":{"x":"y"}},"topologyKey":"zone"}]}})

      assert refused(tmp_dir, ["--set-json", "affinity=#{pod_affinity}"]) =~ "affinity.podAffinity"

      assert refused(tmp_dir, ["--set", "replicas=2,persistence.enabled=false" | @keys]) =~ "persistence.enabled"
    end
  end

  test "S3 defaults to two spread replicas, a PDB, and a NetworkPolicy that keeps the cluster port to its pods", %{
    tmp_dir: tmp_dir
  } do
    assert refused(tmp_dir, @s3) =~ "cluster.cookie"

    docs = render!(tmp_dir, @s3 ++ @keys)
    assert docs[@deployment] =~ "  replicas: 2\n" <> @rollout

    assert docs[@deployment] =~
             "      affinity:\n" <>
               @zone_term <>
               """
                     topologySpreadConstraints:
                       - maxSkew: 1
                         topologyKey: kubernetes.io/hostname
                         whenUnsatisfiable: ScheduleAnyway
                         labelSelector:
                           matchLabels:
                             app: "files-main"
                     containers:
               """

    assert docs[@pdb] =~ """
           spec:
             maxUnavailable: 1
             selector:
               matchLabels:
                 app: "files-main"
           """

    assert String.ends_with?(docs[@network_policy], """
             ingress:
               - ports:
                   - port: http
               - from:
                   - podSelector:
                       matchLabels:
                         app: "files-main"
                 ports:
                   - port: dist
           """)

    refute Map.has_key?(render!(tmp_dir, @s3 ++ @cluster ++ ["--set", "networkPolicy.enabled=false"]), @network_policy)
  end

  test "S3 adds its zone preference after an operator's preferred podAntiAffinity term", %{tmp_dir: tmp_dir} do
    operator =
      ~s({"podAntiAffinity":{"preferredDuringSchedulingIgnoredDuringExecution":[{"weight":50,"podAffinityTerm":{"labelSelector":{"matchLabels":{"tier":"db"}},"topologyKey":"kubernetes.io/hostname"}}]}})

    docs = render!(tmp_dir, @s3 ++ @keys ++ ["--set-json", "affinity=#{operator}"])

    assert docs[@deployment] =~ """
                 affinity:
                   podAntiAffinity:
                     preferredDuringSchedulingIgnoredDuringExecution:
                     - podAffinityTerm:
                         labelSelector:
                           matchLabels:
                             tier: db
                         topologyKey: kubernetes.io/hostname
                       weight: 50
                     - podAffinityTerm:
                         labelSelector:
                           matchLabels:
                             app: files-main
                         topologyKey: topology.kubernetes.io/zone
                       weight: 100
                 topologySpreadConstraints:
           """
  end

  test "refuses a thumbnail cache outside the pod's writable roots", %{tmp_dir: tmp_dir} do
    for dir <- ["/var/thumbs", "/tmpx/thumbs", "/tmp/../app/thumbs", "/cache/thumbs"] do
      assert refused(tmp_dir, ["--set-string", "thumbnail.cacheDir=#{dir}"]) =~ "thumbnail.cacheDir"
    end

    assert refused(tmp_dir, @s3 ++ @keys ++ ["--set-string", "thumbnail.cacheDir=/data/thumbs"]) =~ "thumbnail.cacheDir"

    render!(tmp_dir, ["--set-string", "thumbnail.cacheDir=/tmp/thumbs"])
    render!(tmp_dir, @s3 ++ @keys ++ ["--set-string", "thumbnail.cacheDir=/cache/thumbs"])
  end

  test "probes the health endpoints and drains for 30 s, whatever the upload size", %{tmp_dir: tmp_dir} do
    for args <- [[], ["--set", "upload.maxMB=300"]] do
      deployment = render!(tmp_dir, args)[@deployment]

      assert deployment =~ "      terminationGracePeriodSeconds: 30\n"

      assert deployment =~ """
                       lifecycle:
                         preStop:
                           sleep:
                             seconds: 5
                       startupProbe:
                         httpGet:
                           path: /.app/healthz
                           port: http
                         periodSeconds: 2
                         failureThreshold: 30
                       livenessProbe:
                         httpGet:
                           path: /.app/healthz
                           port: http
                       readinessProbe:
                         httpGet:
                           path: /.app/readyz
                           port: http
             """
    end
  end

  test "runs restricted: non-root, no privileges, a read-only root with /tmp writable, no API token", %{
    tmp_dir: tmp_dir
  } do
    docs = render!(tmp_dir, [])
    deployment = docs[@deployment]

    assert deployment =~ """
                 securityContext:
                   runAsNonRoot: true
                   runAsUser: 1001
                   runAsGroup: 1001
                   fsGroup: 1001
                   seccompProfile:
                     type: RuntimeDefault
           """

    assert deployment =~ """
                     securityContext:
                       allowPrivilegeEscalation: false
                       capabilities:
                         drop:
                           - ALL
                       readOnlyRootFilesystem: true
           """

    assert deployment =~ "            - name: tmp\n              mountPath: /tmp\n"
    assert deployment =~ "        - name: tmp\n          emptyDir: {}\n"
    assert deployment =~ "      serviceAccountName: \"files-main\"\n"
    assert docs[@service_account] =~ ~r/name: "files-main"\n.*\nautomountServiceAccountToken: false\n/s
  end

  describe "existingSecret" do
    test "reads every key from the Secret, each optional unless the shape needs it", %{tmp_dir: tmp_dir} do
      disk = render!(tmp_dir, ["--set-string", "existingSecret=creds"])
      refute Map.has_key?(disk, @secret)
      refute disk[@deployment] =~ "envFrom"
      assert disk[@deployment] =~ secret_env([])

      s3 =
        render!(tmp_dir, [
          "--set-string",
          "storage.backend=s3,s3.bucket=files,existingSecret=creds,auth.username=alice",
          "--set",
          "auth.enabled=true"
        ])

      refute Map.has_key?(s3, @secret)
      refute s3[@deployment] =~ "envFrom"

      assert s3[@deployment] =~
               secret_env(~w(SLIMSERVE_PASSWORD SECRET_KEY_BASE AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY RELEASE_COOKIE))

      assert s3[@deployment] =~ "  replicas: 2\n"
      assert s3[@deployment] =~ @cluster_env
      assert s3[@headless] =~ "clusterIP: None"
    end
  end

  test "labels every resource the standard way and keeps the Deployment selector", %{tmp_dir: tmp_dir} do
    docs =
      render!(
        tmp_dir,
        @s3 ++ @cluster ++ ["--set-string", "auth.username=alice,auth.password=pw", "--set", "persistence.enabled=true"]
      )

    labels =
      ~r/  namespace: "files"\n  labels:\n    app\.kubernetes\.io\/name: slimserve-helm\n    app\.kubernetes\.io\/instance: files\n    app\.kubernetes\.io\/version: "\d+\.\d+\.\d+"\n    app\.kubernetes\.io\/component: server\n    app\.kubernetes\.io\/managed-by: Helm\n    helm\.sh\/chart: slimserve-helm-\d+\.\d+\.\d+\n/

    assert docs |> Map.keys() |> Enum.sort() == [
             "# Source: slimserve-helm/templates/deployment.yml",
             "# Source: slimserve-helm/templates/headless.yml",
             "# Source: slimserve-helm/templates/networkpolicy.yml",
             "# Source: slimserve-helm/templates/pdb.yml",
             "# Source: slimserve-helm/templates/pvc.yml",
             "# Source: slimserve-helm/templates/secret.yml",
             "# Source: slimserve-helm/templates/service.yml",
             "# Source: slimserve-helm/templates/serviceaccount.yml",
             "# Source: slimserve-helm/templates/tests/healthz.yml"
           ]

    for {source, doc} <- docs do
      assert {source, doc =~ labels} == {source, true}
    end

    assert docs[@deployment] =~ "  selector:\n    matchLabels:\n      app: \"files-main\"\n  template:\n"

    assert docs[@deployment] =~
             ~r/      labels:\n        app: "files-main"\n        app\.kubernetes\.io\/name: slimserve-helm\n/
  end

  describe "the values schema" do
    test "rejects a misspelt key", %{tmp_dir: tmp_dir} do
      for {set, key} <- [
            {"replica=2", "replica"},
            {"image.main.tga=x", "tga"},
            {"thumbnail.cacheMb=50", "cacheMb"},
            {"resources.request.main.storage=5Gi", "request"}
          ] do
        assert refused(tmp_dir, ["--set", set]) =~ ~r/schema.*\n.*#{key}/s
      end
    end

    test "passes through what Kubernetes and the Go chart define", %{tmp_dir: tmp_dir} do
      render!(tmp_dir, [
        "--set-string",
        "nodeSelector.disk=ssd,tolerations[0].key=dedicated,tolerations[0].operator=Exists,resources.limits.main.memory=1Gi",
        "--set-string",
        "service.type=NodePort,service.ports[0].name=http,service.ports[0].protocol=TCP,service.ports[0].nodePort=30080",
        "--set",
        "service.ports[0].port=8080,main.ports[0].containerPort=8080,main.auth.enabled=false",
        "--set-string",
        "main.options.slimserve_log_level=info"
      ])
    end

    test "takes the string form a chart 2 release set with --set-string", %{tmp_dir: tmp_dir} do
      docs =
        render!(tmp_dir, [
          "--set-string",
          "persistence.enabled=true,auth.enabled=false,port=8080,upload.maxMB=100,replicas=1"
        ])

      assert docs["# Source: slimserve-helm/templates/pvc.yml"] =~ "kind: PersistentVolumeClaim"
      assert docs[@deployment] =~ "  replicas: 1\n"

      render!(tmp_dir, @s3 ++ ["--set-string", "replicas=1,s3.downloadCacheMB=512"])
    end

    test "accepts the global values a parent chart passes to its subcharts", %{tmp_dir: tmp_dir} do
      parent = Path.join(tmp_dir, "parent")
      File.mkdir_p!(Path.join(parent, "charts"))
      File.cp_r!("manifests/helm", Path.join(parent, "charts/slimserve-helm"))
      File.write!(Path.join(parent, "Chart.yaml"), "apiVersion: v2\nname: parent\nversion: 0.1.0\n")
      File.write!(Path.join(parent, "values.yaml"), "global:\n  imageRegistry: registry.example.test\n")

      assert {out, 0} =
               helm(tmp_dir, ["--set-string", "slimserve-helm.host=files.example.test"], ["template", "files", parent])

      assert out =~ "kind: Deployment"
    end
  end

  test "helm lint passes on both shapes", %{tmp_dir: tmp_dir} do
    assert {_out, 0} = helm(tmp_dir, [], ["lint", "manifests/helm"])
    assert {_out, 0} = helm(tmp_dir, @s3 ++ @keys, ["lint", "manifests/helm"])
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
      drop = preview ++ @s3 ++ ["--set", "persistence.enabled=false,replicas=1"]

      assert {out, 1} = helm(tmp_dir, drop)
      assert out =~ "cannot disable persistence on an upgrade with an existing data claim"
      assert_received {:get, "/api/v1/namespaces/files/persistentvolumeclaims/files-data-pvc"}

      Agent.update(claim?, fn _ -> false end)
      assert {_out, 0} = helm(tmp_dir, drop)
    end

    test "refuses an S3 upgrade with default persistence over an existing data claim, and names the setting", %{
      tmp_dir: tmp_dir,
      claim?: claim?,
      preview: preview
    } do
      args = preview ++ @s3 ++ ["--set", "replicas=1"]

      assert {out, 1} = helm(tmp_dir, args)
      assert out =~ "persistence.enabled=true"
      assert_received {:get, "/api/v1/namespaces/files/persistentvolumeclaims/files-data-pvc"}

      Agent.update(claim?, fn _ -> false end)
      assert {_out, 0} = helm(tmp_dir, args)
    end

    test "keeps the data claim on a local upgrade with default values over an existing claim", %{
      tmp_dir: tmp_dir,
      preview: preview
    } do
      assert {out, 0} = helm(tmp_dir, preview)
      assert out =~ "kind: PersistentVolumeClaim"
    end

    test "keeps the data claim on an S3 upgrade with persistence.enabled=true over an existing claim", %{
      tmp_dir: tmp_dir,
      preview: preview
    } do
      assert {out, 0} = helm(tmp_dir, preview ++ @s3 ++ ["--set", "persistence.enabled=true,replicas=1"])
      assert out =~ "kind: PersistentVolumeClaim"
    end
  end

  @api_resources %{
    "/api/v1" =>
      {"v1",
       [
         {"persistentvolumeclaims", "PersistentVolumeClaim"},
         {"secrets", "Secret"},
         {"serviceaccounts", "ServiceAccount"},
         {"services", "Service"}
       ]},
    "/apis/apps/v1" => {"apps/v1", [{"deployments", "Deployment"}]},
    "/apis/policy/v1" => {"policy/v1", [{"poddisruptionbudgets", "PodDisruptionBudget"}]}
  }

  def init(opts), do: opts

  def call(%{method: "GET", request_path: path} = conn, %{test: test, claim?: claim?}) do
    send(test, {:get, path})

    case path do
      "/version" ->
        json(conn, 200, %{major: "1", minor: "33", gitVersion: "v1.33.0"})

      # Helm 4 reads this as protobuf: an empty body is an empty schema, so the server dry-run skips validation.
      "/openapi/v2" ->
        send_resp(conn, 200, "")

      "/api" ->
        json(conn, 200, %{kind: "APIVersions", versions: ["v1"], serverAddressByClientCIDRs: []})

      "/apis" ->
        apps = %{groupVersion: "apps/v1", version: "v1"}
        policy = %{groupVersion: "policy/v1", version: "v1"}

        json(conn, 200, %{
          kind: "APIGroupList",
          apiVersion: "v1",
          groups: [
            %{name: "apps", versions: [apps], preferredVersion: apps},
            %{name: "policy", versions: [policy], preferredVersion: policy}
          ]
        })

      path when is_map_key(@api_resources, path) ->
        {group_version, kinds} = @api_resources[path]
        json(conn, 200, resources(group_version, kinds))

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
