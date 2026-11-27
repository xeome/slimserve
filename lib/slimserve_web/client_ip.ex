defmodule SlimServeWeb.ClientIp do
  @moduledoc """
  Sets `remote_ip` to the client a trusted proxy reports in X-Forwarded-For, so the login limit
  counts clients, not the proxy. The list is read right to left: each trusted proxy appends the
  address it got the request from, so the first address no trusted proxy holds is the client, and
  anything left of it is the client's own claim.
  """
  @behaviour Plug

  import Bitwise

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    trusted = Application.get_env(:slimserve, :trusted_proxies, [])

    if trusted?(conn.remote_ip, trusted) do
      # A proxy such as HAProxy adds its own header line after one the client sent.
      hops = conn |> Plug.Conn.get_req_header("x-forwarded-for") |> Enum.join(",") |> String.split(",")
      %{conn | remote_ip: client(Enum.reverse(hops), conn.remote_ip, conn.remote_ip, trusted)}
    else
      conn
    end
  end

  # Every hop trusted: the left-most is the client. An entry that is not an address ends the walk at the peer.
  defp client([], last, _peer, _trusted), do: last

  defp client([hop | rest], _last, peer, trusted) do
    case hop |> String.trim() |> address() |> String.to_charlist() |> :inet.parse_strict_address() do
      {:ok, ip} -> if trusted?(ip, trusted), do: client(rest, ip, peer, trusted), else: ip
      {:error, _not_an_address} -> peer
    end
  end

  # Some proxies write the client's port: `203.0.113.9:51234`, `[2001:db8::1]:443`.
  defp address("[" <> rest), do: rest |> String.split("]", parts: 2) |> hd()

  defp address(hop) do
    case String.split(hop, ":") do
      [ipv4, _port] -> ipv4
      _ipv4_or_ipv6 -> hop
    end
  end

  defp trusted?(ip, trusted) do
    ip = unmap(ip)
    Enum.any?(trusted, fn {net, bits} -> tuple_size(net) == tuple_size(ip) and prefix(net, bits) == prefix(ip, bits) end)
  end

  # The dual-stack listener shows an IPv4 peer as ::ffff:a.b.c.d.
  defp unmap({0, 0, 0, 0, 0, 0xFFFF, ab, cd}), do: {ab >>> 8, ab &&& 0xFF, cd >>> 8, cd &&& 0xFF}
  defp unmap(ip), do: ip

  defp prefix(ip, bits) do
    width = if tuple_size(ip) == 4, do: 8, else: 16
    int = ip |> Tuple.to_list() |> Enum.reduce(0, &(&2 <<< width ||| &1))
    int >>> (tuple_size(ip) * width - bits)
  end
end
