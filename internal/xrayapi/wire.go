package xrayapi

// Minimal wire-compatible message descriptors, based on the official Xray
// protobuf files listed in docs/xray-api.md. This imports no Xray core code.
// Local descriptor names are irrelevant on the wire; TypedMessage.type uses
// the canonical official name. Unknown fields are preserved by protobuf.
import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type field struct {
	name     string
	number   int32
	kind     descriptorpb.FieldDescriptorProto_Type
	message  string
	repeated bool
}
type definition struct {
	name   string
	fields []field
}

var wire = makeDescriptors()

func makeDescriptors() protoreflect.FileDescriptor {
	const (
		str     = descriptorpb.FieldDescriptorProto_TYPE_STRING
		data    = descriptorpb.FieldDescriptorProto_TYPE_BYTES
		msg     = descriptorpb.FieldDescriptorProto_TYPE_MESSAGE
		u32     = descriptorpb.FieldDescriptorProto_TYPE_UINT32
		u64     = descriptorpb.FieldDescriptorProto_TYPE_UINT64
		i64     = descriptorpb.FieldDescriptorProto_TYPE_INT64
		boolean = descriptorpb.FieldDescriptorProto_TYPE_BOOL
	)
	defs := []definition{
		{"Empty", nil},
		{"Typed", []field{{"type", 1, str, "", false}, {"value", 2, data, "", false}}},
		{"Account", []field{{"id", 1, str, "", false}, {"flow", 2, str, "", false}}},
		{"User", []field{{"level", 1, u32, "", false}, {"email", 2, str, "", false}, {"account", 3, msg, "Typed", false}}},
		{"AddUser", []field{{"user", 1, msg, "User", false}}},
		{"RemoveUser", []field{{"email", 1, str, "", false}}},
		{"AlterInbound", []field{{"tag", 1, str, "", false}, {"operation", 2, msg, "Typed", false}}},
		{"GetUsers", []field{{"tag", 1, str, "", false}, {"email", 2, str, "", false}}},
		{"Users", []field{{"users", 1, msg, "User", true}}},
		{"StatsQuery", []field{{"pattern", 1, str, "", false}, {"reset", 2, boolean, "", false}}},
		{"Stat", []field{{"name", 1, str, "", false}, {"value", 2, i64, "", false}}},
		{"Stats", []field{{"stat", 1, msg, "Stat", true}}},
		{"Inbound", []field{{"tag", 1, str, "", false}, {"receiver_settings", 2, msg, "Typed", false}, {"proxy_settings", 3, msg, "Typed", false}}},
		{"AddInbound", []field{{"inbound", 1, msg, "Inbound", false}}},
		{"RemoveInbound", []field{{"tag", 1, str, "", false}}},
		{"ListInbounds", []field{{"isOnlyTags", 1, boolean, "", false}}},
		{"Inbounds", []field{{"inbounds", 1, msg, "Inbound", true}}},
		{"PortRange", []field{{"from", 1, u32, "", false}, {"to", 2, u32, "", false}}},
		{"PortList", []field{{"range", 1, msg, "PortRange", true}}},
		{"Address", []field{{"ip", 1, data, "", false}, {"domain", 2, str, "", false}}},
		{"Receiver", []field{{"port_list", 1, msg, "PortList", false}, {"listen", 2, msg, "Address", false}, {"stream_settings", 3, msg, "Stream", false}}},
		{"Stream", []field{{"protocol_name", 5, str, "", false}, {"security_type", 3, str, "", false}, {"security_settings", 4, msg, "Typed", true}}},
		{"Reality", []field{{"show", 1, boolean, "", false}, {"dest", 2, str, "", false}, {"type", 3, str, "", false}, {"xver", 4, u64, "", false}, {"server_names", 5, str, "", true}, {"private_key", 6, data, "", false}, {"short_ids", 10, data, "", true}}},
		{"VLESS", []field{{"users", 1, msg, "User", true}, {"decryption", 3, str, "", false}}},
	}
	file := &descriptorpb.FileDescriptorProto{Name: proto.String("zic_xray_wire.proto"), Package: proto.String("zic.xraywire"), Syntax: proto.String("proto3")}
	for _, d := range defs {
		m := &descriptorpb.DescriptorProto{Name: proto.String(d.name)}
		for _, f := range d.fields {
			label := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL
			if f.repeated {
				label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED
			}
			fd := &descriptorpb.FieldDescriptorProto{Name: proto.String(f.name), Number: proto.Int32(f.number), Type: f.kind.Enum(), Label: label.Enum()}
			if f.message != "" {
				fd.TypeName = proto.String(".zic.xraywire." + f.message)
			}
			m.Field = append(m.Field, fd)
		}
		file.MessageType = append(file.MessageType, m)
	}
	fd, err := protodesc.NewFile(file, nil)
	if err != nil {
		panic(err)
	}
	return fd
}

func message(name string) *dynamicpb.Message {
	return dynamicpb.NewMessage(wire.Messages().ByName(protoreflect.Name(name)))
}
func set(m *dynamicpb.Message, name string, value protoreflect.Value) {
	m.Set(m.Descriptor().Fields().ByName(protoreflect.Name(name)), value)
}
func textField(m protoreflect.Message, name string) string {
	return m.Get(m.Descriptor().Fields().ByName(protoreflect.Name(name))).String()
}
func child(m protoreflect.Message, name string) protoreflect.Message {
	return m.Get(m.Descriptor().Fields().ByName(protoreflect.Name(name))).Message()
}
func list(m protoreflect.Message, name string) protoreflect.List {
	return m.Get(m.Descriptor().Fields().ByName(protoreflect.Name(name))).List()
}
func appendMessage(m *dynamicpb.Message, name string, value *dynamicpb.Message) {
	m.Mutable(m.Descriptor().Fields().ByName(protoreflect.Name(name))).List().Append(protoreflect.ValueOfMessage(value))
}
func typed(name string, value *dynamicpb.Message) *dynamicpb.Message {
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(value)
	if err != nil {
		panic(err)
	} // All descriptors are fixed and values type checked.
	m := message("Typed")
	set(m, "type", protoreflect.ValueOfString(name))
	set(m, "value", protoreflect.ValueOfBytes(b))
	return m
}
