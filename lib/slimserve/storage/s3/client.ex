defmodule SlimServe.Storage.S3.Client do
  @moduledoc """
  No request is retried, because a write that is sent twice is not the write that was checked.
  """

  @type result :: {:ok, Req.Response.t()} | {:error, Exception.t()}

  @spec request(atom(), String.t(), keyword()) :: result()
  def request(method, key, opts \\ []) do
    config = Application.fetch_env!(:slimserve, :s3)
    {query, opts} = Keyword.pop(opts, :query, [])

    result =
      [
        method: method,
        # RFC 3986, not Req's form encoding: SigV4 signs a space in a prefix as %20, never +.
        url:
          "s3://#{config[:bucket]}/" <>
            encode(key) <> if(query == [], do: "", else: "?" <> URI.encode_query(query, :rfc3986)),
        retry: false,
        redirect: false,
        raw: true,
        compressed: false,
        aws_sigv4: Keyword.take(config, [:access_key_id, :secret_access_key, :token, :region])
      ]
      |> Req.new()
      |> ReqS3.attach(aws_endpoint_url_s3: config[:endpoint])
      |> Req.request(opts)

    :telemetry.execute([:slimserve, :s3, :request], %{}, %{method: method, key: key, query: query, result: result})
    result
  end

  def encode(key), do: URI.encode(key, &(&1 == ?/ or URI.char_unreserved?(&1)))

  def copy_source(key), do: "/" <> Application.fetch_env!(:slimserve, :s3)[:bucket] <> "/" <> encode(key)

  def header(%Req.Response{} = resp, name), do: resp |> Req.Response.get_header(name) |> List.first()

  @doc "An element that occurs once is a map, not a list, so callers wrap it."
  def xml(%Req.Response{body: body}) do
    ReqS3.XML.parse_s3(IO.iodata_to_binary(body))
  rescue
    _not_xml -> %{}
  end
end
