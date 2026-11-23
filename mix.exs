defmodule SlimServe.MixProject do
  use Mix.Project

  def project do
    [
      app: :slimserve,
      version: "0.1.0",
      elixir: "~> 1.17",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      aliases: aliases(),
      deps: deps(),
      compilers: [:phoenix_live_view] ++ Mix.compilers(),
      listeners: [Phoenix.CodeReloader],
      test_coverage: [summary: [threshold: 30]],
      dialyzer: [plt_core_path: "priv/plts", plt_local_path: "priv/plts", plt_add_apps: [:ex_unit]]
    ]
  end

  def application do
    [
      mod: {SlimServe.Application, []},
      extra_applications: [:logger, :runtime_tools]
    ]
  end

  def cli do
    [
      preferred_envs: [precommit: :test]
    ]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:phoenix, "~> 1.8.17"},
      {:phoenix_html, "~> 4.1"},
      {:phoenix_live_reload, "~> 1.2", only: :dev},
      {:phoenix_live_view, "~> 1.2.0"},
      {:lazy_html, ">= 0.1.0", only: :test},
      {:phoenix_live_dashboard, "~> 0.8.3"},
      {:esbuild, "~> 0.10", runtime: Mix.env() == :dev},
      {:tailwind, "~> 0.5", runtime: Mix.env() == :dev},
      {:heroicons,
       github: "tailwindlabs/heroicons", tag: "v2.2.0", sparse: "optimized", app: false, compile: false, depth: 1},
      {:telemetry_metrics, "~> 1.0"},
      {:telemetry_poller, "~> 1.0"},
      {:jason, "~> 1.2"},
      {:dns_cluster, "~> 0.2.0"},
      {:bandit, "~> 1.5"},
      {:vix, "~> 0.44.0"},
      {:file_system, "~> 1.1"},
      {:styler, "~> 1.12", only: [:dev, :test], runtime: false},
      {:credo, "~> 1.7", only: [:dev, :test], runtime: false},
      {:dialyxir, "~> 1.4", only: [:dev, :test], runtime: false},
      {:sobelow, "~> 0.19.0", only: [:dev, :test], runtime: false},
      {:mix_audit, "~> 2.1", only: [:dev, :test], runtime: false}
    ]
  end

  defp aliases do
    [
      setup: ["deps.get", "assets.setup", "assets.build"],
      "assets.setup": ["tailwind.install --if-missing", "esbuild.install --if-missing"],
      "assets.build": ["compile", "tailwind slimserve", "esbuild slimserve"],
      "assets.deploy": [
        "tailwind slimserve --minify",
        "esbuild slimserve --minify",
        "phx.digest"
      ],
      # hex.audit goes first: after compile, Mix no longer finds the Hex archive's tasks.
      precommit: [
        "hex.audit",
        "compile --warnings-as-errors",
        "deps.unlock --check-unused",
        "format --check-formatted",
        "credo --strict",
        "sobelow --exit low --skip",
        &check_sobelow_skips/1,
        "deps.audit",
        "dialyzer",
        "test --cover --warnings-as-errors",
        "cmd npx --yes @fission-ai/openspec@2.1.1 validate --specs --strict"
      ]
    ]
  end

  # `sobelow --skip` obeys every skip comment, every skip attribute and both files below, so each needs the
  # owner's approval. The pattern needs `#` or `@` right before the name, so its own literal here never matches.
  defp check_sobelow_skips(_args) do
    {out, status} =
      System.cmd("git", [
        "-c",
        "core.quotePath=false",
        "grep",
        "--untracked",
        "--no-color",
        "-I",
        "-E",
        "(#[[:space:]]*|@)sobelow_skip",
        "--",
        ".",
        ":!.sobelow-skips-allowed"
      ])

    if status not in [0, 1], do: Mix.raise("git grep failed: #{out}")

    found =
      for line <- String.split(out, "\n", trim: true) do
        [path, text] = String.split(line, ":", parts: 2)
        [_, checks] = String.split(text, "sobelow_skip", parts: 2)
        path <> ": " <> String.trim(checks)
      end

    allowed =
      ".sobelow-skips-allowed"
      |> File.read!()
      |> String.split("\n")
      |> Enum.map(&String.trim/1)
      |> Enum.reject(&(&1 == "" or String.starts_with?(&1, "#")))

    problems =
      Enum.map(found -- allowed, &"not in .sobelow-skips-allowed: #{&1}") ++
        Enum.map(allowed -- found, &"in .sobelow-skips-allowed but not in the code: #{&1}") ++
        for(file <- [".sobelow-skips", ".sobelow-conf"], File.exists?(file), do: "#{file} exists and silences sobelow")

    if problems != [], do: Mix.raise(Enum.join(["sobelow skips differ from the approved set:" | problems], "\n  "))
  end
end
